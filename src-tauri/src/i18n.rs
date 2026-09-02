//! 간단 i18n: 설정의 lang("ko"|"en")에 따라 피드/알림 문자열을 반환한다.
//! lang에 없는 값은 ko로 폴백한다.

pub fn is_en(lang: &str) -> bool {
    lang == "en"
}

/// {} 자리표시자 포맷 (format!은 리터럴 포맷 문자열만 허용하므로 함수로 제공)
pub fn fmt(lang: &str, ko: &str, en: &str, args: &[&dyn std::fmt::Display]) -> String {
    let mut out = if is_en(lang) { en.to_string() } else { ko.to_string() };
    for a in args {
        out = out.replacen("{}", &a.to_string(), 1);
    }
    out
}

pub fn tr(lang: &str, ko: &str, en: &str) -> String {
    if is_en(lang) { en.to_string() } else { ko.to_string() }
}

/// 편의용 매크로 (함수 tr의 위임)
#[macro_export]
macro_rules! tr {
    ($lang:expr, $ko:expr, $en:expr) => {
        $crate::i18n::tr($lang.as_ref(), $ko, $en)
    };
}
