#ifndef RunnerAXSnapshotBridge_h
#define RunnerAXSnapshotBridge_h

#import <Foundation/Foundation.h>
#import <XCTest/XCTest.h>

NS_ASSUME_NONNULL_BEGIN

/**
 * Private-accessibility snapshot fallback for deep React Native trees.
 *
 * The public `XCUIElement.snapshot()` serializer throws `kAXErrorIllegalArgument`
 * (or returns a childless root) once a React Navigation screen's tree crosses a
 * size-dependent limit, and the runner's traversal then returns an EMPTY tree —
 * this is the whole devicelab-vs-WDA React Native gap (native-* flows failing
 * `assertVisible` on content that is really on screen).
 *
 * This bridge asks XCTest's private `XCAXClient_iOS` for the tree directly, and
 * re-roots the request from depth-capped frontier nodes: the AX server's depth
 * limit is PER REQUEST, so re-rooting at a frontier element's live accessibility
 * element reaches content the app-rooted request could not, without ever passing
 * a larger depth. Adapted from callstack/agent-device's `RunnerAXSnapshotBridge`,
 * trimmed to the capture mechanism (no custom-action reads).
 *
 * Simulator-and-device safe: every private call is guarded, and any failure
 * returns `{ok: NO, error: ...}` so the caller falls back to the empty payload
 * it would have produced anyway. Never throws.
 *
 * Response: `{ok: YES, root: <nested node dict>, truncated: <bool>}` on success,
 * `{ok: NO, error: <string>}` on failure. A node dict is
 * `{type: <int elementType>, identifier, label, value, frame: {x,y,width,height},
 *   enabled, selected, focused, children: [<node dict>...]}`.
 */
@interface RunnerAXSnapshotBridge : NSObject

/// Captures @c application's accessibility tree via the private AX client and
/// chains element-rooted follow-up requests from depth-capped frontier nodes.
/// @c maxDepth bounds each request; @c maxNodes bounds the whole capture;
/// @c deepExtensionCallLimit bounds re-root requests (0 disables extension);
/// @c timeout seconds bounds the whole capture (<= 0 means no wall-clock bound).
+ (NSDictionary<NSString *, id> *)snapshotTreeForApplication:(XCUIApplication *)application
                                                    maxDepth:(NSInteger)maxDepth
                                                    maxNodes:(NSInteger)maxNodes
                                      deepExtensionCallLimit:(NSInteger)deepExtensionCallLimit
                                                     timeout:(NSTimeInterval)timeout;

@end

NS_ASSUME_NONNULL_END

#endif /* RunnerAXSnapshotBridge_h */
