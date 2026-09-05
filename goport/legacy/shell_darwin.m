// dock-util Go 포트용 네이티브 셸 (Objective-C)
// NSPanel(nonactivating) + WKWebView 조합으로 오브/팝오버를 띄운다.
// Rust 포트의 panel.rs + platform.rs + 일부 ipc 역할을 한 파일이가 담당한다.
// 모든 NSWindow 조작은 GCD 메인 큐에서 실행한다 (macOS 26 백그라운드 setLevel 트랩 회피).

#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>
#import <Carbon/Carbon.h>
#import <CoreGraphics/CoreGraphics.h>
#include <stdlib.h>
#include <string.h>
#include "_cgo_export.h"

static NSString *gUIDir = nil;
static NSMutableDictionary *gPanels = nil; // panelId(NSNumber) -> DUWinObj

// ---------- 레지스트리 객체 ----------

@interface DUPanelDelegate : NSObject <NSWindowDelegate>
@property (assign) long long panelId;
@end

@implementation DUPanelDelegate
- (void)windowDidResignKey:(NSNotification *)n {
  dockutil_on_blur(self.panelId);
}
- (void)windowWillClose:(NSNotification *)n {
  [gPanels removeObjectForKey:@(self.panelId)];
  dockutil_on_closed(self.panelId);
}
@end

@interface DUMessageHandler : NSObject <WKScriptMessageHandler>
@property (assign) long long panelId;
@end

@implementation DUMessageHandler
- (void)userContentController:(WKUserContentController *)ucc
      didReceiveScriptMessage:(WKScriptMessage *)msg {
  if (![msg.body isKindOfClass:[NSDictionary class]]) return;
  NSData *d = [NSJSONSerialization dataWithJSONObject:msg.body options:0 error:nil];
  if (!d) return;
  NSString *json = [[NSString alloc] initWithData:d encoding:NSUTF8StringEncoding];
  @autoreleasepool {
    dockutil_on_message(self.panelId, (char *)json.UTF8String);
  }
}
@end

// 페이지 로드 진단용
@interface DUNavDelegate : NSObject <WKNavigationDelegate>
@end
@implementation DUNavDelegate
- (void)webView:(WKWebView *)webView didFinishNavigation:(WKNavigation *)nav {
  NSLog(@"[nav] didFinish %@", webView.URL);
}
- (void)webView:(WKWebView *)webView didFailNavigation:(WKNavigation *)nav withError:(NSError *)err {
  NSLog(@"[nav] FAIL %@", err.localizedDescription);
}
- (void)webView:(WKWebView *)webView didFailProvisionalNavigation:(WKNavigation *)nav withError:(NSError *)err {
  NSLog(@"[nav] PROVISIONAL FAIL %@", err.localizedDescription);
}
@end

@interface DUWinObj : NSObject
@property (assign) NSPanel *panel;
@property (assign) WKWebView *web;
@property (strong) DUPanelDelegate *del; // NSWindow.delegate는 weak이므로 직접 보유
@property (strong) DUMessageHandler *mh;
@property (strong) DUNavDelegate *nav;
@end
@implementation DUWinObj
@end

// 파일 드래그&드롭을 가로채는 WKWebView (Tauri drag-drop 이벤트와 동일하게 Go로 전달)
@interface DUWebView : WKWebView
- (NSArray<NSURL *> *)fileURLsFrom:(id<NSDraggingInfo>)sender;
@end

@implementation DUWebView
- (instancetype)initWithFrame:(NSRect)frame configuration:(WKWebViewConfiguration *)cfg {
  self = [super initWithFrame:frame configuration:cfg];
  if (self) [self registerForDraggedTypes:@[NSPasteboardTypeFileURL]];
  return self;
}
- (NSArray<NSURL *> *)fileURLsFrom:(id<NSDraggingInfo>)sender {
  return [[sender draggingPasteboard] readObjectsForClasses:@[[NSURL class]]
                                                   options:@{NSPasteboardURLReadingFileURLsOnlyKey: @YES}];
}
- (NSDragOperation)draggingEntered:(id<NSDraggingInfo>)sender {
  if ([self fileURLsFrom:sender].count > 0) return NSDragOperationCopy;
  return [super draggingEntered:sender];
}
- (NSDragOperation)draggingUpdated:(id<NSDraggingInfo>)sender {
  if ([self fileURLsFrom:sender].count > 0) return NSDragOperationCopy;
  return [super draggingUpdated:sender];
}
- (BOOL)performDragOperation:(id<NSDraggingInfo>)sender {
  NSArray *urls = [self fileURLsFrom:sender];
  if (urls.count > 0) {
    NSMutableArray *paths = [NSMutableArray array];
    for (NSURL *u in urls) [paths addObject:u.path ?: @""];
    NSData *d = [NSJSONSerialization dataWithJSONObject:paths options:0 error:nil];
    NSString *json = d ? [[NSString alloc] initWithData:d encoding:NSUTF8StringEncoding] : @"[]";
    long long pid = 0;
    for (NSNumber *k in gPanels) {
      DUWinObj *w = gPanels[k];
      if (w.web == self) { pid = k.longLongValue; break; }
    }
    if (pid > 0) dockutil_on_drop(pid, (char *)json.UTF8String);
    return YES;
  }
  return [super performDragOperation:sender];
}
@end

// ---------- 유틸 ----------

static void withPanel(long long id, void (^block)(DUWinObj *)) {
  dispatch_async(dispatch_get_main_queue(), ^{
    @autoreleasepool {
      DUWinObj *w = gPanels[@(id)];
      if (w) block(w);
    }
  });
}

// ---------- 앱 생명주기 ----------

@interface DUAppDelegate : NSObject <NSApplicationDelegate>
@end
@implementation DUAppDelegate
- (void)applicationDidFinishLaunching:(NSNotification *)n {
  dispatch_async(dispatch_get_main_queue(), ^{ dockutil_on_ready(); });
}
@end

static EventHotKeyRef gHotRef = NULL;

static OSStatus hotkeyHandler(EventHandlerCallRef call, EventRef ev, void *ud) {
  if (GetEventClass(ev) == kEventClassKeyboard && GetEventKind(ev) == kEventHotKeyPressed) {
    dockutil_on_hotkey();
    return noErr;
  }
  return eventNotHandledErr;
}

static void installHotkey(void) {
  EventTypeSpec spec = {kEventClassKeyboard, kEventHotKeyPressed};
  InstallEventHandler(GetApplicationEventTarget(), hotkeyHandler, 1, &spec, NULL, NULL);
  EventHotKeyID hid = {'duwk', 1};
  RegisterEventHotKey(13 /*W*/, cmdKey | optionKey, hid, GetApplicationEventTarget(), 0, &gHotRef);
}

// 메인 고루틴에서 호출 — [NSApp run] 안에서 영원히 블록된다
void du_app_run(const char *uiDir) {
  gUIDir = [[NSString alloc] initWithUTF8String:uiDir ?: ""];
  gPanels = [NSMutableDictionary dictionary];
  NSLog(@"[shell] uiDir=%@", gUIDir);
  [NSApplication sharedApplication];
  [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
  installHotkey();
  DUAppDelegate *ad = [DUAppDelegate new];
  NSApp.delegate = ad;
  [NSApp run];
}

void du_quit(void) {
  dispatch_async(dispatch_get_main_queue(), ^{
    [NSApp terminate:nil];
  });
}

// ---------- 패널 생성/조작 ----------

void du_panel_create(long long id, const char *label, const char *shim,
                     double x, double y, double w, double h, int ignores, long long level) {
  NSString *lbl = [[NSString alloc] initWithUTF8String:label ?: ""];
  NSString *shimSrc = [[NSString alloc] initWithUTF8String:shim ?: ""];
  dispatch_async(dispatch_get_main_queue(), ^{
    @autoreleasepool {
      NSRect frame = NSMakeRect(x, y, w, h);
      // 1<<7 = NSWindowStyleMaskNonactivatingPanel, borderless(0)
      NSPanel *panel = [[NSPanel alloc] initWithContentRect:frame
                                                  styleMask:(1u << 7)
                                                    backing:NSBackingStoreBuffered
                                                      defer:NO];
      [panel setOpaque:NO];
      [panel setBackgroundColor:[NSColor clearColor]];
      [panel setHasShadow:NO];
      [panel setIgnoresMouseEvents:ignores ? YES : NO];
      [panel setLevel:level];
      [panel setReleasedWhenClosed:NO];
      // canJoinAllSpaces | stationary | fullScreenAuxiliary | ignoresCycle
      [panel setCollectionBehavior:(1u << 0) | (1u << 4) | (1u << 8) | (1u << 10)];

      WKWebViewConfiguration *cfg = [[WKWebViewConfiguration alloc] init];
      [cfg setValue:@NO forKey:@"drawsBackground"];
      [cfg.preferences setValue:@YES forKey:@"developerExtrasEnabled"];
      WKUserContentController *ucc = [[WKUserContentController alloc] init];
      // 주의: C 문자열(shim)은 Go가 함수 반환 즉시 해제하므로,
      // 블록 안에서는 반드시 미리 변환해 둔 NSString(shimSrc)을 사용한다.
      NSString *src = shimSrc ?: @"";
      WKUserScript *script = [[WKUserScript alloc] initWithSource:src
                                                    injectionTime:WKUserScriptInjectionTimeAtDocumentStart
                                                  forMainFrameOnly:YES];
      [ucc addUserScript:script];
      DUMessageHandler *mh = [[DUMessageHandler alloc] init];
      mh.panelId = id;
      [ucc addScriptMessageHandler:mh name:@"duInvoke"];
      cfg.userContentController = ucc;

      DUWebView *web = [[DUWebView alloc] initWithFrame:frame configuration:cfg];
      [web setValue:@NO forKey:@"drawsBackground"];
      web.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;

      DUPanelDelegate *del = [[DUPanelDelegate alloc] init];
      del.panelId = id;
      panel.delegate = del;

      DUWinObj *win = [[DUWinObj alloc] init];
      win.panel = panel;
      win.web = web;
      win.del = del;
      win.mh = mh;
      win.nav = [[DUNavDelegate alloc] init];
      web.navigationDelegate = win.nav;
      gPanels[@(id)] = win;

      [panel setContentView:web];
      [panel orderFrontRegardless];

      NSURL *dir = [NSURL fileURLWithPath:gUIDir];
      [web loadFileURL:[dir URLByAppendingPathComponent:@"index.html"] allowingReadAccessToURL:dir];
    }
  });
}

void du_panel_close(long long id) {
  withPanel(id, ^(DUWinObj *w) {
    [w.panel orderOut:nil];
    [w.panel close];
  });
}

// 프레임 + 레벨/컬렉션 재단언 + 전면 표시 + 앱 활성화 (Rust apply_overlay 동일)
void du_panel_apply(long long id, double x, double y, double w, double h, long long level) {
  dispatch_async(dispatch_get_main_queue(), ^{
    @autoreleasepool {
      DUWinObj *win = gPanels[@(id)];
      if (!win) return;
      NSPanel *p = win.panel;
      [p setFrame:NSMakeRect(x, y, w, h) display:YES];
      [p setLevel:level];
      [p setCollectionBehavior:(1u << 0) | (1u << 4) | (1u << 8) | (1u << 10)];
      [p orderFrontRegardless];
      [[NSApplication sharedApplication] activateIgnoringOtherApps:YES];
    }
  });
}

void du_panel_set_ignores(long long id, int ignore) {
  withPanel(id, ^(DUWinObj *w) { [w.panel setIgnoresMouseEvents:ignore ? YES : NO]; });
}

void du_panel_pin_spaces(long long id) {
  withPanel(id, ^(DUWinObj *w) {
    [w.panel setCollectionBehavior:(1u << 0) | (1u << 8)];
  });
}

void du_panel_focus(long long id) {
  withPanel(id, ^(DUWinObj *w) {
    [w.panel orderFrontRegardless];
    [w.panel makeKeyAndOrderFront:nil];
    [[NSApplication sharedApplication] activateIgnoringOtherApps:YES];
  });
}

void du_eval(long long id, const char *js) {
  NSString *code = [[NSString alloc] initWithUTF8String:js ?: ""];
  withPanel(id, ^(DUWinObj *w) {
    [w.web evaluateJavaScript:code completionHandler:nil];
  });
}

// ---------- 좌표/화면 ----------

double du_mouse_x(void) { return [NSEvent mouseLocation].x; }
double du_mouse_y(void) { return [NSEvent mouseLocation].y; }

void du_main_screen(double *w, double *h) {
  CGRect b = CGDisplayBounds(CGMainDisplayID());
  *w = b.size.width;
  *h = b.size.height;
}

int du_screen_count(void) {
  uint32_t n = 0;
  if (CGGetActiveDisplayList(0, NULL, &n) != kCGErrorSuccess) return 0;
  return (int)n;
}

// AppKit 좌하단 원점으로 변환된 i번째 화면 프레임 (Rust all_screens 동일)
int du_screen_at(int i, double *x, double *y, double *w, double *h) {
  uint32_t n = 0;
  if (CGGetActiveDisplayList(0, NULL, &n) != kCGErrorSuccess) return 0;
  if (i < 0 || (uint32_t)i >= n || n == 0) return 0;
  CGDirectDisplayID *ids = (CGDirectDisplayID *)malloc(sizeof(CGDirectDisplayID) * n);
  if (CGGetActiveDisplayList(n, ids, &n) != kCGErrorSuccess) { free(ids); return 0; }
  CGFloat ph = 0;
  int hasPrimary = 0;
  for (uint32_t k = 0; k < n; k++) {
    CGRect b = CGDisplayBounds(ids[k]);
    if (b.origin.x == 0 && b.origin.y == 0) { ph = b.size.height; hasPrimary = 1; break; }
  }
  CGRect b = CGDisplayBounds(ids[i]);
  free(ids);
  if (!hasPrimary) return 0;
  *x = b.origin.x;
  *y = ph - b.origin.y - b.size.height;
  *w = b.size.width;
  *h = b.size.height;
  return 1;
}

// ---------- 접근성: Dock 프레임 ----------

static CFStringRef cfs(const char *s) {
  return CFStringCreateWithCString(kCFAllocatorDefault, s ?: "", kCFStringEncodingUTF8);
}

static CGPoint axPoint(CFTypeRef v) {
  CGPoint p = {0, 0};
  if (v) {
    AXValueGetValue((AXValueRef)v, kAXValueCGPointType, &p);
    CFRelease(v);
  }
  return p;
}

static CGSize axSize(CFTypeRef v) {
  CGSize s = {0, 0};
  if (v) {
    AXValueGetValue((AXValueRef)v, kAXValueCGSizeType, &s);
    CFRelease(v);
  }
  return s;
}

static CFArrayRef copyArray(CFTypeRef el, const char *attr) {
  CFTypeRef out = NULL;
  CFStringRef name = cfs(attr);
  AXError err = AXUIElementCopyAttributeValue((AXUIElementRef)el, name, &out);
  CFRelease(name);
  if (err != kAXErrorSuccess || !out) return NULL;
  return (CFArrayRef)out;
}

// AXPosition/AXSize를 읽어 사각형으로 반환. 성공 시 1.
static int readAXRect(AXUIElementRef el, double *x, double *y, double *w, double *h) {
  CFTypeRef pv = NULL, sv = NULL;
  CFStringRef pn = cfs("AXPosition"), sn = cfs("AXSize");
  int ok = 0;
  if (AXUIElementCopyAttributeValue(el, pn, &pv) == kAXErrorSuccess && pv) {
    CGPoint pt = axPoint(pv); // axPoint가 pv 소유권 처리
    if (AXUIElementCopyAttributeValue(el, sn, &sv) == kAXErrorSuccess && sv) {
      CGSize sz = axSize(sv);
      *x = pt.x; *y = pt.y; *w = sz.width; *h = sz.height;
      ok = 1;
    }
  }
  CFRelease(pn);
  CFRelease(sn);
  return ok;
}

int du_ax_trusted(void) { return AXIsProcessTrustedWithOptions(NULL) ? 1 : 0; }

// pid는 Go가 pgrep으로 캐싱해서 넘긴다. 성공 시 1.
int du_dock_frame(int pid, double *x, double *y, double *w, double *h) {
  if (!AXIsProcessTrustedWithOptions(NULL)) return 0;
  int found = 0;
  @autoreleasepool {
    AXUIElementRef app = AXUIElementCreateApplication((pid_t)pid);
    if (app) {
      // 1순위: AXDocks (macOS 26 이전)
      CFArrayRef docks = copyArray(app, "AXDocks");
      if (docks) {
        if (CFArrayGetCount(docks) > 0) {
          AXUIElementRef dock = (AXUIElementRef)CFArrayGetValueAtIndex(docks, 0);
          if (readAXRect(dock, x, y, w, h)) found = 1;
        }
        CFRelease(docks);
      }
      // 2순위: macOS 26+ AXDocks 제거 → AXChildren에서 큰 사각형 폴백
      if (!found) {
        CFArrayRef children = copyArray(app, "AXChildren");
        if (children) {
          CFIndex n = CFArrayGetCount(children);
          for (CFIndex i = 0; i < n && !found; i++) {
            AXUIElementRef c = (AXUIElementRef)CFArrayGetValueAtIndex(children, i);
            double cx, cy, cw, ch;
            if (readAXRect(c, &cx, &cy, &cw, &ch) && cw > 100 && ch > 10) {
              *x = cx; *y = cy; *w = cw; *h = ch;
              found = 1;
            }
          }
          CFRelease(children);
        }
      }
      CFRelease(app);
    }
  }
  return found;
}

// ---------- 클립보드 ----------

// malloc된 UTF8 문자열 반환 (Go에서 free)
char *du_clip_text(void) {
  __block char *out = NULL;
  dispatch_sync(dispatch_get_main_queue(), ^{
    @autoreleasepool {
      NSString *s = [[NSPasteboard generalPasteboard] stringForType:NSPasteboardTypeString] ?: @"";
      out = strdup(s.UTF8String ?: "");
    }
  });
  return out ?: strdup("");
}

void du_clip_set_text(const char *s) {
  NSString *str = [[NSString alloc] initWithUTF8String:s ?: ""];
  dispatch_async(dispatch_get_main_queue(), ^{
    @autoreleasepool {
      NSPasteboard *pb = [NSPasteboard generalPasteboard];
      [pb clearContents];
      [pb setString:str forType:NSPasteboardTypeString];
    }
  });
}

// 파일 경로(JSON 배열)를 파일 참조로 클립보드에 복사. 성공 1.
int du_copy_file_refs(const char *jsonPaths) {
  NSString *str = [[NSString alloc] initWithUTF8String:jsonPaths ?: "[]"];
  NSData *data = [str dataUsingEncoding:NSUTF8StringEncoding];
  id parsed = [NSJSONSerialization JSONObjectWithData:data options:0 error:nil];
  if (![parsed isKindOfClass:[NSArray class]]) return 0;
  NSMutableArray *urls = [NSMutableArray array];
  for (NSString *p in parsed) {
    if ([p isKindOfClass:[NSString class]]) {
      NSURL *u = [NSURL fileURLWithPath:p];
      if (u) [urls addObject:u];
    }
  }
  if (urls.count == 0) return 0;
  __block int ok = 0;
  dispatch_sync(dispatch_get_main_queue(), ^{
    @autoreleasepool {
      NSPasteboard *pb = [NSPasteboard generalPasteboard];
      [pb clearContents];
      ok = [pb writeObjects:urls] ? 1 : 0;
    }
  });
  return ok;
}

// ---------- 폴더 선택 (비동기 — 메인 루프 막지 않음) ----------

void du_pick_folder(long long reqId, const char *title) {
  NSString *t = [[NSString alloc] initWithUTF8String:title ?: ""];
  dispatch_async(dispatch_get_main_queue(), ^{
    @autoreleasepool {
      NSOpenPanel *p = [NSOpenPanel openPanel];
      p.canChooseDirectories = YES;
      p.canChooseFiles = NO;
      p.allowsMultipleSelection = NO;
      p.title = t;
      [p beginWithCompletionHandler:^(NSModalResponse r) {
        NSString *path = @"";
        if (r == NSModalResponseOK && p.URLs.firstObject) path = p.URLs.firstObject.path ?: @"";
        dockutil_on_folder(reqId, (char *)path.UTF8String);
      }];
    }
  });
}

// ---------- QA: 마우스 이벤트 시뮬레이션 ----------

static void postMouseEvent(uint32_t type, double x, double y) {
  CGPoint pt = CGPointMake(x, y);
  CGEventRef ev = CGEventCreateMouseEvent(NULL, (CGEventType)type, pt, kCGMouseButtonLeft);
  if (ev) {
    CGEventPost(kCGSessionEventTap, ev);
    CFRelease(ev);
  }
}

void du_post_mouse_move(double x, double y) { postMouseEvent(kCGEventMouseMoved, x, y); }

void du_post_mouse_click(double x, double y) {
  postMouseEvent(kCGEventLeftMouseDown, x, y);
  usleep(50 * 1000);
  postMouseEvent(kCGEventLeftMouseUp, x, y);
}
