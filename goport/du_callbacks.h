#ifndef DU_CALLBACKS_H
#define DU_CALLBACKS_H
// Go 실행파일이 익스포트하는 콜백 — Swift dylib이 dynamic_lookup으로 바인딩한다.
#include <stdint.h>

void dockutil_on_ready(void);
void dockutil_on_message(long long id, const char *json);
void dockutil_on_blur(long long id);
void dockutil_on_closed(long long id);
void dockutil_on_hotkey(int kind);
void dockutil_on_folder(long long reqId, const char *path);
void dockutil_on_drop(long long id, const char *jsonPaths);
void dockutil_on_space_change(void);

#endif
