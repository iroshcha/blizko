import Foundation

struct Contact: Decodable, Identifiable {
    let id: String
    let name: String
    let address: String
}
struct ChatMessage: Decodable, Identifiable {
    let id: String
    let peer: String
    let text: String
    let out: Bool
    let delivered: Bool
    let time: Int64
}
struct Snapshot: Decodable {
    var status: String = "Открываем хранилище…"
    var enabled = false
    var online = false
    var authURL = ""
    var address = ""
    var id = ""
    var contacts: [Contact] = []
    var messages: [ChatMessage] = []
    var incoming = 0
}

@MainActor final class ChatModel: ObservableObject {
    @Published var snapshot = Snapshot()
    @Published var error: String?
    @Published var ready = false
    private var node: BlizkoBridge?
    private let queue = DispatchQueue(label: "ru.blizko.core")
    private var timer: Timer?
    private var polling = false

    init() {
        queue.async { [weak self] in
            do {
                var dir = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0].appendingPathComponent("Blizko")
                let exists = FileManager.default.fileExists(atPath: dir.appendingPathComponent("vault").path)
                let key = try StorageKey.load(existingVault: exists)
                try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
                var values = URLResourceValues(); values.isExcludedFromBackup = true
                try dir.setResourceValues(values)
                try FileManager.default.setAttributes([.protectionKey: FileProtectionType.completeUntilFirstUserAuthentication], ofItemAtPath: dir.path)
                let node = BlizkoBridge.make(path: dir.path, key: key)
                if let failure = node.failure { throw NSError(domain: "Blizko", code: 4, userInfo: [NSLocalizedDescriptionKey: failure]) }
                DispatchQueue.main.async { self?.node = node; self?.ready = true; self?.refresh() }
            } catch {
                DispatchQueue.main.async { self?.error = "Не удалось открыть защищённое хранилище. Данные не удалены." }
            }
        }
        timer = Timer.scheduledTimer(withTimeInterval: 1, repeats: true) { [weak self] _ in
            Task { @MainActor in self?.refresh() }
        }
    }
    func refresh() {
        guard let node, !polling else { return }; polling = true
        queue.async { [weak self] in
            let raw = node.snapshot()
            let next = try? JSONDecoder().decode(Snapshot.self, from: Data(raw.utf8))
            DispatchQueue.main.async { if let next { self?.snapshot = next }; self?.polling = false }
        }
    }
    private func perform(_ name: String, first: String = "", second: String = "", success: ((String) -> Void)? = nil) {
        guard let node else { return }
        queue.async { [weak self] in
            do {
                let raw = node.command(name, first: first, second: second)
                let result = try JSONSerialization.jsonObject(with: Data(raw.utf8)) as? [String: Any]
                DispatchQueue.main.async {
                    if let error = result?["error"] as? String { self?.error = error }
                    else { success?(result?["code"] as? String ?? ""); self?.refresh() }
                }
            } catch { DispatchQueue.main.async { self?.error = "Не удалось обработать действие" } }
        }
    }
    func toggle() { perform(snapshot.enabled ? "stop" : "start") }
    func add(name: String, code: String, success: @escaping () -> Void) { perform("add", first: name, second: code) { _ in success() } }
    func send(peer: String, text: String, success: @escaping () -> Void) { perform("send", first: peer, second: text) { _ in success() } }
    func myCode(_ receive: @escaping (String) -> Void) {
        perform("code", success: receive)
    }
}
