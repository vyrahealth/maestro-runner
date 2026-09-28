#import "SnapshotParams.h"

#import <objc/runtime.h>

// Original implementation of -[XCAXClient_iOS defaultParameters], captured when
// we swap in our own so we can start from XCTest's real defaults and only
// override the keys we care about.
static NSDictionary *(*dl_original_defaultParameters)(id, SEL) = NULL;

// Bounded generous depth. INT_MAX invites runaway cost on pathological trees.
// 100 clears the deep native wrapper nesting a React Native (Fabric / New
// Architecture) screen produces — 60 could still clip content nested inside a
// ScrollView, the same class of gap WebDriverAgent's default cap of 50 causes
// (#171). Override with DEVICELAB_SNAPSHOT_MAX_DEPTH for an unusually deep app.
static int dl_snapshotMaxDepth(void) {
  static int depth = 0;
  static dispatch_once_t once;
  dispatch_once(&once, ^{
    depth = 100;
    const char *env = getenv("DEVICELAB_SNAPSHOT_MAX_DEPTH");
    if (env != NULL) {
      int parsed = atoi(env);
      if (parsed > 0) {
        depth = parsed;
      }
    }
  });
  return depth;
}

static id dl_swizzled_defaultParameters(id self, SEL _cmd) {
  NSDictionary *original =
      dl_original_defaultParameters ? dl_original_defaultParameters(self, _cmd) : @{};
  NSMutableDictionary *merged =
      [NSMutableDictionary dictionaryWithDictionary:(original ?: @{})];
  merged[@"maxDepth"] = @(dl_snapshotMaxDepth());
  merged[@"maxChildren"] = @(INT_MAX);
  merged[@"maxArrayCount"] = @(INT_MAX);
  // 0 == do NOT honor modal views, i.e. keep elements behind a modal/dialog in
  // the snapshot. Our own hittable/occlusion pass still marks obscured elements
  // as non-hittable, so this only adds completeness.
  merged[@"snapshotKeyHonorModalViews"] = @0;
  return [merged copy];
}

void DLApplyCompleteSnapshotParams(void) {
  static dispatch_once_t once;
  dispatch_once(&once, ^{
    Class cls = NSClassFromString(@"XCAXClient_iOS");
    if (cls == Nil) {
      NSLog(@"DL_SNAPSHOT_PARAMS: XCAXClient_iOS unavailable; snapshot params unchanged");
      return;
    }
    Method method =
        class_getInstanceMethod(cls, NSSelectorFromString(@"defaultParameters"));
    if (method == NULL) {
      NSLog(@"DL_SNAPSHOT_PARAMS: -defaultParameters unavailable; snapshot params unchanged");
      return;
    }
    dl_original_defaultParameters = (NSDictionary * (*)(id, SEL))
        method_setImplementation(method, (IMP)dl_swizzled_defaultParameters);
    NSLog(@"DL_SNAPSHOT_PARAMS: applied (maxDepth=%d, maxChildren=INT_MAX, honorModalViews=0)",
          dl_snapshotMaxDepth());
  });
}
