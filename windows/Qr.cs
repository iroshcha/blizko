using System;
using System.Drawing;
using System.Drawing.Imaging;
using System.IO;
using System.Windows.Media.Imaging;
using ZXing;
using ZXing.QrCode;
using ZXing.QrCode.Internal;

namespace Blizko {
    internal static class Qr {
        internal static string Require(string value) {
            value = (value ?? "").Trim();
            if (value.StartsWith("blizko:2:", StringComparison.Ordinal)) throw new IOException("Это старый QR. Обновите приложения и обменяйтесь новыми кодами.");
            if (!value.StartsWith("blizko:3:", StringComparison.Ordinal) || value.Length > 4096) throw new IOException("Нужен QR контакта «Близко».");
            return value;
        }
        internal static Bitmap Create(string code) {
            return new BarcodeWriter { Format = BarcodeFormat.QR_CODE,
                Options = new QrCodeEncodingOptions { Width = 360, Height = 360, Margin = 4, ErrorCorrection = ErrorCorrectionLevel.M }
            }.Write(Require(code));
        }
        internal static BitmapSource Source(Bitmap bitmap) {
            using (var stream = new MemoryStream()) {
                bitmap.Save(stream, ImageFormat.Png); stream.Position = 0;
                var image = new BitmapImage(); image.BeginInit(); image.CacheOption = BitmapCacheOption.OnLoad;
                image.StreamSource = stream; image.EndInit(); image.Freeze(); return image;
            }
        }
        internal static string Decode(string path) {
            if (new FileInfo(path).Length > 30 * 1024 * 1024) throw new IOException("Выберите изображение меньше 30 МБ.");
            using (var original = new Bitmap(path)) {
                if ((long)original.Width * original.Height > 100000000) throw new IOException("Изображение слишком большое.");
                double ratio = Math.Min(1.0, 2048.0 / Math.Max(original.Width, original.Height));
                using (var bitmap = new Bitmap(original, Math.Max(1, (int)(original.Width * ratio)), Math.Max(1, (int)(original.Height * ratio)))) {
                    var reader = new BarcodeReader { AutoRotate = true };
                    reader.Options.TryInverted = true;
                    reader.Options.TryHarder = true;
                    reader.Options.PossibleFormats = new[] { BarcodeFormat.QR_CODE };
                    var results = reader.DecodeMultiple(bitmap);
                    string found = null;
                    if (results != null) foreach (var result in results) {
                        if (result.Text.StartsWith("blizko:3:", StringComparison.Ordinal)) {
                            if (found != null) throw new IOException("На картинке несколько QR. Выберите изображение с одним контактом.");
                            found = Require(result.Text);
                        }
                    }
                    if (found == null) throw new IOException("Не найден QR контакта «Близко». Выберите чёткую картинку с одним кодом.");
                    return found;
                }
            }
        }
    }
}
