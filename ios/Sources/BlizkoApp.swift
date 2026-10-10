import SwiftUI
import PhotosUI

private let forest = Color(red: 0.14, green: 0.42, blue: 0.35)
private let paper = Color(red: 0.96, green: 0.965, blue: 0.95)

@MainActor @main struct BlizkoApp: App {
    @StateObject private var model = ChatModel()
    var body: some Scene { WindowGroup { HomeView().environmentObject(model).tint(forest) } }
}

struct HomeView: View {
    @EnvironmentObject var model: ChatModel
    @Environment(\.openURL) private var openURL
    @State private var adding = false
    @State private var code = ""
    @State private var showingCode = false
    @State private var info = false
    @State private var updates = false
    @State private var networkSetup = false
    var body: some View {
        NavigationStack {
            VStack(alignment: .leading, spacing: 16) {
                Text("Личное остаётся у вас").foregroundStyle(.secondary)
                VStack(alignment: .leading, spacing: 12) {
                    Label(model.snapshot.status, systemImage: model.snapshot.online ? "circle.fill" : "circle")
                        .font(.subheadline).foregroundStyle(forest)
                    Button(model.snapshot.enabled ? "Выключить приём" : "Подключиться") { model.toggle() }
                        .disabled(!model.ready)
                    Button("Настройки соединения") { networkSetup = true }
                }.padding().frame(maxWidth: .infinity, alignment: .leading).background(.white, in: RoundedRectangle(cornerRadius: 16))
                HStack {
                    Button { model.myCode { code = $0; showingCode = true } } label: { Label("Мой QR", systemImage: "qrcode") }
                    Spacer()
                    Button { adding = true } label: { Label("Контакт", systemImage: "plus") }
                }.disabled(!model.ready)
                Button("Обновления") { updates = true }.font(.subheadline)
                Text("ПЕРЕПИСКИ").font(.caption).tracking(2).foregroundStyle(.secondary)
                if model.snapshot.contacts.isEmpty {
                    VStack(alignment: .leading, spacing: 10) {
                        Text("Ваш первый разговор").font(.title2).bold()
                        Text("Включите приём и обменяйтесь QR в обе стороны. Регистрация не нужна. После обновления со старой версии нужны новые QR; история сохраняется.").foregroundStyle(.secondary)
                    }.padding(.top, 24)
                    Spacer()
                } else {
                    ScrollView {
                        LazyVStack(spacing: 10) {
                            ForEach(model.snapshot.contacts) { contact in
                                NavigationLink { ConversationView(contact: contact) } label: {
                                    VStack(alignment: .leading, spacing: 7) {
                                        Text(contact.name).font(.headline).foregroundStyle(.primary)
                                        Text(model.snapshot.previews[contact.id] ?? "Начать разговор")
                                            .lineLimit(1).font(.subheadline).foregroundStyle(.secondary)
                                    }.padding().frame(maxWidth: .infinity, alignment: .leading).background(.white, in: RoundedRectangle(cornerRadius: 16))
                                }.buttonStyle(.plain)
                            }
                        }
                    }
                }
                Text("На iPhone приём может приостановиться после сворачивания. Откройте приложение для доставки.")
                    .font(.caption).foregroundStyle(.secondary)
            }.padding(.horizontal, 22).padding(.bottom).background(paper)
                .navigationTitle("Близко")
                .toolbar { Button { info = true } label: { Image(systemName: "info.circle") } }
                .sheet(isPresented: $adding) { AddContactView() }
                .sheet(isPresented: $showingCode) { ContactQRView(code: code) }
                .sheet(isPresented: $networkSetup) { QRConnectionView() }
                .alert("Близко", isPresented: Binding(get: { model.error != nil }, set: { if !$0 { model.error = nil } })) { Button("Понятно") { model.error = nil } } message: { Text(model.error ?? "") }
                .alert("Как работает чат", isPresented: $info) { Button("Понятно", role: .cancel) {} } message: {
                    Text("iroh встроен: аккаунт и VPN не нужны. История и очередь отправки находятся только на телефонах. Если прямое соединение невозможно, зашифрованные данные проходят через ваш домашний сервер. Он должен быть включён и подключён к интернету.\n\nУдаление приложения удаляет историю. Резервной копии нет. Это прототип без независимого аудита безопасности.")
                }
                .alert("Обновления на iPhone", isPresented: $updates) { Button("Понятно", role: .cancel) {} } message: {
                    Text("При бесплатной подписи новая версия устанавливается через SideStore или Sideloadly. Готовый IPA нужно скачать из артефактов успешной macOS-сборки на GitHub. Для сохранения переписки устанавливайте поверх текущей версии с тем же Apple Account и идентификатором приложения.")
                }
        }
    }
}

struct AddContactView: View {
    @EnvironmentObject var model: ChatModel
    @Environment(\.dismiss) var dismiss
    @Environment(\.openURL) private var openURL
    @State private var name = ""
    @State private var code = ""
    @State private var scanning = false
    @State private var photo: PhotosPickerItem?
    @State private var loading = false
    var body: some View {
        NavigationStack {
            Form {
                Button { scanning = true } label: { Label("Сканировать QR", systemImage: "qrcode.viewfinder") }
                PhotosPicker(selection: $photo, matching: .images) { Label("Выбрать QR из фото", systemImage: "photo") }.disabled(loading)
                if loading { ProgressView("Читаем QR…") }
                if !code.isEmpty {
                    Label("QR контакта считан", systemImage: "checkmark.circle")
                    TextField("Имя собеседника", text: $name)
                }
                Text("Добавление доступно только через QR. Другу нужно добавить ваш QR тоже. Включите приём на обоих телефонах.").font(.caption)
                Button("Добавить") {
                    model.add(name: name, code: code) { dismiss() }
                }.disabled(name.trimmingCharacters(in: .whitespaces).isEmpty || code.isEmpty)
            }.navigationTitle("Новый контакт").toolbar { Button("Отмена") { dismiss() } }
                .sheet(isPresented: $scanning) {
                    NavigationStack { QRScanner { value in code = value; scanning = false }
                        .navigationTitle("Сканировать QR").toolbar { Button("Закрыть") { scanning = false } } }
                }
                .onChange(of: photo) { item in
                    guard let item else { return }; loading = true; code = ""
                    Task {
                        do {
                            if let data = try await item.loadTransferable(type: Data.self), let value = await Task.detached(operation: { ContactQR.decode(data) }).value { code = value }
                            else { model.error = "На изображении не найден QR контакта «Близко». Выберите картинку с одним чётким кодом." }
                        } catch { model.error = "Не удалось открыть изображение." }
                        loading = false; photo = nil
                    }
                }
                .alert("Близко", isPresented: Binding(get: { model.error != nil }, set: { if !$0 { model.error = nil } })) { Button("Понятно") { model.error = nil } } message: { Text(model.error ?? "") }
        }
    }
}

struct QRConnectionView: View {
    @EnvironmentObject var model: ChatModel
    @Environment(\.dismiss) private var dismiss
    var body: some View {
        NavigationStack {
            Form {
                Toggle("Только через ретранслятор", isOn: Binding(get: { model.snapshot.relayOnly }, set: { model.setRelayOnly($0) }))
                Text("Режим проверки: все сообщения идут через ретранслятор iroh с шифрованием. В обычном режиме приложение пробует прямое соединение автоматически.")
            }.navigationTitle("Соединение iroh").toolbar { Button("Готово") { dismiss() } }
        }
    }
}

struct ConversationView: View {
    @EnvironmentObject var model: ChatModel
    let contact: Contact
    @State private var draft = ""
    @State private var clearing = false
    private var messages: [ChatMessage] { model.snapshot.messages.filter { $0.peer == contact.id } }
    var body: some View {
        VStack(spacing: 0) {
            Text(model.snapshot.status).font(.caption).foregroundStyle(.secondary).padding(8)
            Button("Проверить связь с собеседником") { model.checkContact(contact.id) }.font(.subheadline)
            HStack {
                Button("Раньше") { model.older() }.disabled(!model.snapshot.hasMore)
                Spacer()
                Button("Последние") { model.recent() }.disabled(model.before == 0)
                Spacer()
                Button("Очистить") { clearing = true }
            }.font(.subheadline).padding(.horizontal)
            if let issue = model.snapshot.deliveryIssues?[contact.id] { Text(issue).font(.caption).foregroundStyle(.secondary).padding(.horizontal) }
            ScrollViewReader { proxy in
                ScrollView {
                    LazyVStack(spacing: 10) {
                        if messages.isEmpty { Text("Добавьте QR-коды контактов на обоих телефонах, чтобы начать разговор.").foregroundStyle(.secondary).padding() }
                        ForEach(messages) { message in
                            HStack {
                                if message.out { Spacer(minLength: 30) }
                                VStack(alignment: .leading, spacing: 6) {
                                    Text(message.text).textSelection(.enabled)
                                    HStack(spacing: 5) {
                                        Text(Date(timeIntervalSince1970: Double(message.time) / 1000), style: .time)
                                        if message.out { Text(message.delivered ? "· Доставлено" : "· В очереди · не доставлено") }
                                    }.font(.caption2).foregroundStyle(.secondary)
                                }.padding(12).background(message.out ? Color(red: 0.86, green: 0.93, blue: 0.89) : .white, in: RoundedRectangle(cornerRadius: 16))
                                if !message.out { Spacer(minLength: 30) }
                            }.id(message.id)
                        }
                    }.padding()
                }.onChange(of: messages.last?.id) { _ in if model.before == 0, let id = messages.last?.id { proxy.scrollTo(id, anchor: .bottom) } }
            }
            HStack {
                TextField("Сообщение…", text: $draft, axis: .vertical).lineLimit(1...4).padding(12).background(.white, in: RoundedRectangle(cornerRadius: 16))
                Button { let text = draft; model.send(peer: contact.id, text: text) { draft = "" } } label: { Image(systemName: "arrow.up.circle.fill").font(.largeTitle) }
                    .disabled(draft.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            }.padding()
        }.background(paper).navigationTitle(contact.name).navigationBarTitleDisplayMode(.inline)
            .onAppear { model.select(contact.id) }.onDisappear { model.select("") }
            .alert("Очистить историю?", isPresented: $clearing) {
                Button("Отмена", role: .cancel) {}
                Button("Очистить", role: .destructive) { model.clear(contact.id) }
            } message: { Text("Будут удалены тексты доставленных сообщений на этом устройстве. Ожидающие отправки сообщения сохранятся.") }
    }
}
