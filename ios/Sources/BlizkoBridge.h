#import <Foundation/Foundation.h>
NS_ASSUME_NONNULL_BEGIN
@interface BlizkoBridge : NSObject
@property(nonatomic, readonly, nullable) NSString *failure;
+ (instancetype)makeWithPath:(NSString *)path key:(NSData *)key NS_SWIFT_NAME(make(path:key:));
- (NSString *)snapshot NS_SWIFT_NAME(snapshot());
- (NSString *)status NS_SWIFT_NAME(status());
- (NSString *)page:(NSString *)peer before:(int64_t)before NS_SWIFT_NAME(page(_:before:));
// JSON result: {"ok":true,"code":"..."} or {"error":"..."}.
- (NSString *)command:(NSString *)name first:(NSString *)first second:(NSString *)second NS_SWIFT_NAME(command(_:first:second:));
@end
NS_ASSUME_NONNULL_END
