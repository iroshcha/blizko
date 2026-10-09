import SwiftUI
import AVFoundation
import CoreImage
import ImageIO
import UIKit

enum ContactQR {
    static func valid(_ value: String) -> Bool { value.hasPrefix("blizko:2:") && value.utf8.count <= 4096 }
    static func image(_ code: String) -> UIImage? {
        guard valid(code), let filter = CIFilter(name: "CIQRCodeGenerator") else { return nil }
        filter.setValue(Data(code.utf8), forKey: "inputMessage")
        filter.setValue("M", forKey: "inputCorrectionLevel")
        guard let output = filter.outputImage else { return nil }
        let scaled = output.transformed(by: CGAffineTransform(scaleX: 8, y: 8))
        guard let cg = CIContext().createCGImage(scaled, from: scaled.extent) else { return nil }
        // Explicit white quiet zone survives image sharing and dark mode.
        let size = CGSize(width: CGFloat(cg.width + 64), height: CGFloat(cg.height + 64))
        let format = UIGraphicsImageRendererFormat(); format.scale = 1; format.opaque = true
        return UIGraphicsImageRenderer(size: size, format: format).image { context in
            UIColor.white.setFill(); context.fill(CGRect(origin: .zero, size: size))
            context.cgContext.interpolationQuality = .none
            UIImage(cgImage: cg).draw(in: CGRect(x: 32, y: 32, width: CGFloat(cg.width), height: CGFloat(cg.height)))
        }
    }
    static func decode(_ data: Data) -> String? {
        guard data.count <= 30_000_000, let source = CGImageSourceCreateWithData(data as CFData, nil),
              let cg = CGImageSourceCreateThumbnailAtIndex(source, 0, [
                kCGImageSourceCreateThumbnailFromImageAlways: true,
                kCGImageSourceCreateThumbnailWithTransform: true,
                kCGImageSourceThumbnailMaxPixelSize: 2048
              ] as CFDictionary),
              let detector = CIDetector(ofType: CIDetectorTypeQRCode, context: CIContext(), options: [CIDetectorAccuracy: CIDetectorAccuracyHigh]) else { return nil }
        let values = detector.features(in: CIImage(cgImage: cg)).compactMap { ($0 as? CIQRCodeFeature)?.messageString }.filter(valid)
        return values.count == 1 ? values[0] : nil
    }
}

struct ContactQRView: View {
    let code: String
    @Environment(\.dismiss) private var dismiss
    @State private var sharing = false
    var body: some View {
        NavigationStack {
            VStack(spacing: 20) {
                if let image = ContactQR.image(code) {
                    Image(uiImage: image).interpolation(.none).resizable().scaledToFit().frame(maxWidth: 360)
                        .accessibilityLabel("QR-код моего контакта")
                    Text("Собеседник сканирует этот QR. Затем добавьте его QR у себя. Если вы не рядом, отправьте картинку.")
                    Button("Поделиться QR") { sharing = true }.buttonStyle(.borderedProminent)
                        .sheet(isPresented: $sharing) { QRShareSheet(image: image) }
                } else { Text("Не удалось создать QR-код.") }
            }.padding().navigationTitle("Мой QR-код").toolbar { Button("Готово") { dismiss() } }
        }
    }
}

private struct QRShareSheet: UIViewControllerRepresentable {
    let image: UIImage
    func makeUIViewController(context: Context) -> UIActivityViewController { UIActivityViewController(activityItems: [image], applicationActivities: nil) }
    func updateUIViewController(_ uiViewController: UIActivityViewController, context: Context) {}
}

struct QRScanner: UIViewControllerRepresentable {
    let found: (String) -> Void
    func makeUIViewController(context: Context) -> QRScannerController { let c = QRScannerController(); c.found = found; return c }
    func updateUIViewController(_ uiViewController: QRScannerController, context: Context) {}
    static func dismantleUIViewController(_ uiViewController: QRScannerController, coordinator: ()) { uiViewController.stop() }
}

final class QRScannerController: UIViewController, AVCaptureMetadataOutputObjectsDelegate {
    var found: ((String) -> Void)?
    private let session = AVCaptureSession()
    private let queue = DispatchQueue(label: "ru.blizko.qr-camera")
    private var preview: AVCaptureVideoPreviewLayer?
    private var delivered = false
    private var visible = false
    private var configured = false
    private let hint = UILabel()
    override func viewDidLoad() {
        super.viewDidLoad(); view.backgroundColor = .black
        hint.text = "Наведите камеру на QR контакта «Близко»"; hint.textColor = .white
        hint.backgroundColor = UIColor.black.withAlphaComponent(0.7); hint.textAlignment = .center; hint.numberOfLines = 0
        view.addSubview(hint)
    }
    override func viewDidAppear(_ animated: Bool) {
        super.viewDidAppear(animated); visible = true
        switch AVCaptureDevice.authorizationStatus(for: .video) {
        case .authorized: configure()
        case .notDetermined: AVCaptureDevice.requestAccess(for: .video) { [weak self] granted in
            DispatchQueue.main.async { if granted { self?.configure() } else { self?.denied() } }
        }
        default: denied()
        }
    }
    private func denied() { hint.text = "Разрешите камеру в настройках или закройте сканер и выберите QR из фото." }
    private func configure() {
        guard visible, !configured else { return }
        guard let camera = AVCaptureDevice.default(for: .video), let input = try? AVCaptureDeviceInput(device: camera), session.canAddInput(input) else {
            hint.text = "Камера недоступна. Выберите QR из фото."; return
        }
        session.addInput(input)
        let output = AVCaptureMetadataOutput()
        guard session.canAddOutput(output) else { hint.text = "Сканер недоступен. Выберите QR из фото."; return }
        session.addOutput(output); output.setMetadataObjectsDelegate(self, queue: .main); output.metadataObjectTypes = [.qr]
        let layer = AVCaptureVideoPreviewLayer(session: session); layer.videoGravity = .resizeAspectFill
        view.layer.insertSublayer(layer, at: 0); preview = layer; configured = true; view.setNeedsLayout()
        queue.async { [session] in session.startRunning() }
    }
    override func viewDidLayoutSubviews() {
        super.viewDidLayoutSubviews(); preview?.frame = view.bounds
        if let connection = preview?.connection, connection.isVideoOrientationSupported,
           let orientation = view.window?.windowScene?.interfaceOrientation {
            switch orientation {
            case .landscapeLeft: connection.videoOrientation = .landscapeLeft
            case .landscapeRight: connection.videoOrientation = .landscapeRight
            case .portraitUpsideDown: connection.videoOrientation = .portraitUpsideDown
            default: connection.videoOrientation = .portrait
            }
        }
        hint.frame = CGRect(x: 16, y: view.safeAreaInsets.top + 16, width: view.bounds.width - 32, height: 100)
    }
    func metadataOutput(_ output: AVCaptureMetadataOutput, didOutput objects: [AVMetadataObject], from connection: AVCaptureConnection) {
        guard !delivered, let value = (objects.first as? AVMetadataMachineReadableCodeObject)?.stringValue else { return }
        guard ContactQR.valid(value) else { hint.text = "Это не QR контакта «Близко». Наведите камеру на другой код."; return }
        delivered = true; stop(); found?(value)
    }
    func stop() { visible = false; queue.async { [session] in if session.isRunning { session.stopRunning() } } }
    override func viewDidDisappear(_ animated: Bool) { super.viewDidDisappear(animated); stop() }
}
