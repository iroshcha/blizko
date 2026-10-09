import Foundation
import Security

enum StorageKey {
    static func load(existingVault: Bool) throws -> Data {
        let query: [String: Any] = [kSecClass as String: kSecClassGenericPassword,
            kSecAttrService as String: "ru.blizko.chat.storage", kSecAttrAccount as String: "master-v2"]
        var find = query
        find[kSecReturnData as String] = true
        find[kSecMatchLimit as String] = kSecMatchLimitOne
        var result: CFTypeRef?
        let status = SecItemCopyMatching(find as CFDictionary, &result)
        if status == errSecSuccess, let data = result as? Data, data.count == 32 { return data }
        guard status == errSecItemNotFound, !existingVault else {
            throw NSError(domain: "Blizko", code: 1, userInfo: [NSLocalizedDescriptionKey: "Ключ хранилища недоступен. История не удалена."])
        }
        var bytes = [UInt8](repeating: 0, count: 32)
        guard SecRandomCopyBytes(kSecRandomDefault, bytes.count, &bytes) == errSecSuccess else { throw NSError(domain: "Blizko", code: 2) }
        let data = Data(bytes)
        var add = query
        add[kSecValueData as String] = data
        add[kSecAttrAccessible as String] = kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly
        guard SecItemAdd(add as CFDictionary, nil) == errSecSuccess else { throw NSError(domain: "Blizko", code: 3) }
        return data
    }
}
