//! 포매터: JSON / YAML / TOML 파싱 → pretty / minify / 상호 변환

/// 모든 포맷을 serde_json::Value로 통일해서 다룬다
fn parse_as_json_value(text: &str, format: &str) -> Result<serde_json::Value, String> {
    match format {
        "yaml" => {
            let v: serde_yaml::Value =
                serde_yaml::from_str(text).map_err(|e| format!("YAML 파싱 실패: {e}"))?;
            serde_json::to_value(v).map_err(|e| e.to_string())
        }
        "toml" => {
            let v: toml::Value =
                toml::from_str(text).map_err(|e| format!("TOML 파싱 실패: {e}"))?;
            serde_json::to_value(v).map_err(|e| e.to_string())
        }
        "auto" => {
            // JSON → TOML → YAML (TOML은 YAML 스칼라로도 파싱되므로 먼저 시도) 순서로 시도
            if let Ok(v) = serde_json::from_str::<serde_json::Value>(text) {
                return Ok(v);
            }
            if let Ok(v) = toml::from_str::<toml::Value>(text) {
                if let Ok(v) = serde_json::to_value(v) {
                    return Ok(v);
                }
            }
            if let Ok(v) = serde_yaml::from_str::<serde_yaml::Value>(text) {
                if let Ok(v) = serde_json::to_value(v) {
                    return Ok(v);
                }
            }
            Err("형식 자동 감지 실패: JSON/YAML/TOML 이 아닙니다".into())
        }
        _ => serde_json::from_str(text).map_err(|e| format!("JSON 파싱 실패: {e}")),
    }
}

fn render(value: &serde_json::Value, format: &str, pretty: bool) -> Result<String, String> {
    match format {
        "yaml" => serde_yaml::to_string(value).map_err(|e| format!("YAML 직렬화 실패: {e}")),
        "toml" => {
            if pretty {
                toml::to_string_pretty(value).map_err(|e| format!("TOML 직렬화 실패: {e}"))
            } else {
                toml::to_string(value).map_err(|e| format!("TOML 직렬화 실패: {e}"))
            }
        }
        _ => {
            if pretty {
                serde_json::to_string_pretty(value).map_err(|e| e.to_string())
            } else {
                serde_json::to_string(value).map_err(|e| e.to_string())
            }
        }
    }
}

/// format: 입력 형식, convert: Some(출력 형식)이면 변환, None이면 같은 형식으로 정리
pub fn format_text(text: &str, format: &str, pretty: bool, convert: Option<&str>) -> Result<String, String> {
    let value = parse_as_json_value(text, format)?;
    let out_fmt = convert.unwrap_or(format);
    render(&value, out_fmt, pretty)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn json_pretty_and_minify() {
        let out = format_text(r#"{"a":1,"b":[1,2]}"#, "json", true, None).unwrap();
        assert!(out.contains("\n  \"a\": 1"));
        let min = format_text(&out, "json", false, None).unwrap();
        assert_eq!(min, r#"{"a":1,"b":[1,2]}"#);
    }

    #[test]
    fn json_to_yaml_and_back() {
        let yaml = format_text(r#"{"a":1,"b":[1,2]}"#, "json", true, Some("yaml")).unwrap();
        assert!(yaml.contains("a: 1"));
        let json = format_text(&yaml, "yaml", true, Some("json")).unwrap();
        let v: serde_json::Value = serde_json::from_str(&json).unwrap();
        assert_eq!(v["b"][1], 2);
    }

    #[test]
    fn auto_detects_toml_before_yaml_scalar() {
        assert_eq!(format_text("answer = 42", "auto", false, Some("json")).unwrap(), r#"{"answer":42}"#);
    }

    #[test]
    fn toml_to_json() {
        let out = format_text("a = 1\n[b]\nc = \"x\"\n", "toml", true, Some("json")).unwrap();
        let v: serde_json::Value = serde_json::from_str(&out).unwrap();
        assert_eq!(v["b"]["c"], "x");
    }

    #[test]
    fn invalid_input_reports_parse_error() {
        let err = format_text("{nope", "json", true, None).unwrap_err();
        assert!(err.contains("JSON 파싱 실패"));
    }
}
