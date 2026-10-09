#import "BlizkoBridge.h"
@import Mobile;

@interface BlizkoBridge ()
@property(nonatomic, strong) MobileNode *node;
@property(nonatomic, readwrite, copy) NSString *failure;
@end
@implementation BlizkoBridge
+ (instancetype)makeWithPath:(NSString *)path key:(NSData *)key {
    BlizkoBridge *bridge = [BlizkoBridge new];
    NSError *error = nil;
    bridge.node = MobileNewNode(path, key, &error);
    if (!bridge.node) bridge.failure = error.localizedDescription ?: @"Не удалось открыть хранилище";
    return bridge;
}
- (NSString *)snapshot { return self.node ? [self.node snapshot] : @"{}"; }
- (NSString *)command:(NSString *)name first:(NSString *)first second:(NSString *)second {
    NSError *error = nil; NSString *code = nil;
    if (!self.node) return @"{\"error\":\"Хранилище недоступно\"}";
    if ([name isEqualToString:@"start"]) [self.node start:&error];
    else if ([name isEqualToString:@"stop"]) [self.node stop];
    else if ([name isEqualToString:@"add"]) [self.node addContact:first code:second error:&error];
    else if ([name isEqualToString:@"send"]) [self.node send:first text:second error:&error];
    else if ([name isEqualToString:@"code"]) code = [self.node myCode:&error];
    else error = [NSError errorWithDomain:@"Blizko" code:1 userInfo:@{NSLocalizedDescriptionKey:@"Неизвестная команда"}];
    NSDictionary *result = error ? @{ @"error": error.localizedDescription } : @{ @"ok": @YES, @"code": code ?: @"" };
    NSData *data = [NSJSONSerialization dataWithJSONObject:result options:0 error:nil];
    return [[NSString alloc] initWithData:data encoding:NSUTF8StringEncoding];
}
@end
