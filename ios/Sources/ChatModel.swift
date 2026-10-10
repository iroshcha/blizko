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
    let order: Int64
}
struct Snapshot: Decodable {
    var status: String = "Открываем хранилище…"
    var enabled = false
    var online = false
    var address = ""
    var id = ""
    var contacts: [Contact] = []
    var messages: [ChatMessage] = []
    var incoming = 0
    var relay = ""
    var relayOnly = false
    var deliveryIssues: [String: String]?
    var revision: UInt64 = 0
    var hasMore = false
    var previews: [String: String] = [:]
}

@MainActor final class ChatModel: ObservableObject {
    @Published var snapshot = Snapshot()
    @Published var error: String?
    @Published var ready = false
    private var node: BlizkoBridge?
    private let queue = DispatchQueue(label: "ru.blizko.core")
    private var timer: Timer?
    private var polling = false
    private var peer = ""
    private(set) var before: Int64 = 0
    private var dirty = true

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
        let target = peer, cursor = before, known = snapshot.revision, force = dirty
        queue.async { [weak self] in
            let status = try? JSONSerialization.jsonObject(with: Data(node.status().utf8)) as? [String: Any]
            let revision = (status?["revision"] as? NSNumber)?.uint64Value
            let next: Snapshot? = force || revision != known ? (try? JSONDecoder().decode(Snapshot.self, from: Data(node.page(target, before: cursor).utf8))) : nil
            DispatchQueue.main.async {
                guard let self else { return }; self.polling = false
                guard self.peer == target && self.before == cursor else { self.refresh(); return }
                if let next { self.snapshot = next; self.dirty = false }
            }
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
    func select(_ peer: String) { self.peer = peer; before = 0; dirty = true; refresh() }
    func older() { if let row = snapshot.messages.first { before = row.order; dirty = true; refresh() } }
    func recent() { before = 0; dirty = true; refresh() }
    func clear(_ peer: String) { perform("clear", first: peer) { [weak self] _ in self?.recent() } }
    func setRelayOnly(_ value: Bool) { perform("relay", first: value ? "true" : "false") }
    func checkContact(_ peer: String) { perform("check", first: peer) { [weak self] result in self?.error = result } }
    func add(name: String, code: String, success: @escaping () -> Void) { perform("add", first: name, second: code) { _ in success() } }
    func send(peer: String, text: String, success: @escaping () -> Void) { perform("send", first: peer, second: text) { _ in success() } }
    func myCode(_ receive: @escaping (String) -> Void) {
        perform("code", success: receive)
    }
}
