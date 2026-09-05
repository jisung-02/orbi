package main

// 간단 i18n: lang("ko"|"en")에 따라 피드/알림 문자열 선택. 없는 값은 ko로 폴백.

import "strings"

func isEn(lang string) bool { return lang == "en" }

func tr(lang, ko, en string) string {
	if isEn(lang) {
		return en
	}
	return ko
}

// fmtTr: {} 자리표시자 순서대로 치환
func fmtTr(lang, ko, en string, args ...string) string {
	out := tr(lang, ko, en)
	for _, a := range args {
		out = strings.Replace(out, "{}", a, 1)
	}
	return out
}
