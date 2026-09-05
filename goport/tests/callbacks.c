#include "../du_callbacks.h"
#include <stdlib.h>
#include <string.h>
static char *last_message;
const char *du_test_last_message(void) { return last_message ? last_message : ""; }
void dockutil_on_ready(void) {}
void dockutil_on_message(long long id, const char *json) { free(last_message); last_message = strdup(json); }
void dockutil_on_blur(long long id) {}
void dockutil_on_closed(long long id) {}
void dockutil_on_hotkey(int kind) {}
void dockutil_on_folder(long long id, const char *path) {}
void dockutil_on_drop(long long id, const char *paths) {}
void dockutil_on_space_change(void) {}
