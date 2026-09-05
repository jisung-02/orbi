package main

// 포매터: JSON / YAML / TOML 파싱 → pretty / minify / 상호 변환
// (Rust widgets/formatter.rs 포트. Rust는 serde_json BTreeMap 특성상 JSON 키가
// 정렬되므로, Go 포트도 encoding/json의 map 정렬 특성과 동일하게 동작한다.)

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

// 모든 포맷을 any(JSON 값)로 통일해서 다룬다
func parseAsJSONValue(text, format string) (any, error) {
	switch format {
	case "yaml":
		var v any
		if err := yaml.Unmarshal([]byte(text), &v); err != nil {
			return nil, fmt.Errorf("YAML 파싱 실패: %v", err)
		}
		return v, nil
	case "toml":
		var v map[string]any
		if err := toml.Unmarshal([]byte(text), &v); err != nil {
			return nil, fmt.Errorf("TOML 파싱 실패: %v", err)
		}
		return v, nil
	case "auto":
		// JSON → TOML → YAML: TOML을 YAML 문자열로 오인하지 않도록 순서 유지
		var jv any
		if err := decodeJSON(text, &jv); err == nil {
			return jv, nil
		}
		var tv map[string]any
		if err := toml.Unmarshal([]byte(text), &tv); err == nil {
			return tv, nil
		}
		var yv any
		if err := yaml.Unmarshal([]byte(text), &yv); err == nil {
			return yv, nil
		}
		return nil, fmt.Errorf("형식 자동 감지 실패: JSON/YAML/TOML 이 아닙니다")
	default:
		var v any
		if err := decodeJSON(text, &v); err != nil {
			return nil, fmt.Errorf("JSON 파싱 실패: %v", err)
		}
		return v, nil
	}
}

func render(value any, format string, pretty bool) (string, error) {
	switch format {
	case "yaml":
		b, err := yaml.Marshal(jsonNumbers(value))
		if err != nil {
			return "", fmt.Errorf("YAML 직렬화 실패: %v", err)
		}
		return string(b), nil
	case "toml":
		b, err := toml.Marshal(jsonNumbers(value))
		if err != nil {
			return "", fmt.Errorf("TOML 직렬화 실패: %v", err)
		}
		return string(b), nil
	default:
		var b bytes.Buffer
		enc := json.NewEncoder(&b)
		enc.SetEscapeHTML(false)
		if pretty {
			enc.SetIndent("", "  ")
		}
		if err := enc.Encode(value); err != nil {
			return "", err
		}
		// Encode는 끝에 개행 추가 — serde_json과 맞추기 위해 제거
		out := b.Bytes()
		if len(out) > 0 && out[len(out)-1] == '\n' {
			out = out[:len(out)-1]
		}
		return string(out), nil
	}
}

// formatText: format=입력 형식, convert가 비어 있으면 같은 형식으로 정리
func formatText(text, format string, pretty bool, convert string) (string, error) {
	value, err := parseAsJSONValue(text, format)
	if err != nil {
		return "", err
	}
	outFmt := convert
	if outFmt == "" {
		outFmt = format
	}
	return render(value, outFmt, pretty)
}

// 정수 ID가 float64 변환으로 반올림되지 않도록 숫자 타입을 보존한다.
func decodeJSON(text string, value *any) error {
	if !json.Valid([]byte(text)) {
		return fmt.Errorf("invalid JSON")
	}
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	if err := dec.Decode(value); err != nil {
		return err
	}
	return nil
}

func jsonNumbers(value any) any {
	switch v := value.(type) {
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return n
		}
		if n, err := strconv.ParseUint(string(v), 10, 64); err == nil {
			return n
		}
		// 부동소수/범위 밖 숫자는 JSON 출력에서 원문을 보존한다.
		if n, err := v.Float64(); err == nil {
			return n
		}
		return v
	case map[string]any:
		for k, item := range v {
			v[k] = jsonNumbers(item)
		}
	case []any:
		for i, item := range v {
			v[i] = jsonNumbers(item)
		}
	}
	return value
}
