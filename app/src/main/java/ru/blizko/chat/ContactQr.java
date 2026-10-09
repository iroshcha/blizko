package ru.blizko.chat;

import android.content.Context;
import android.graphics.Bitmap;
import android.graphics.BitmapFactory;
import android.net.Uri;
import androidx.core.content.FileProvider;
import com.google.zxing.*;
import com.google.zxing.common.BitMatrix;
import com.google.zxing.common.HybridBinarizer;
import com.google.zxing.qrcode.QRCodeReader;
import com.google.zxing.qrcode.QRCodeWriter;
import com.google.zxing.qrcode.decoder.ErrorCorrectionLevel;
import java.io.*;
import java.util.*;

/** QR transports the contact key/address and optionally an owner-approved Tailscale share invitation. */
final class ContactQr {
    static String requireContact(String value) {
        if (value == null || !value.startsWith("blizko:2:") || value.length() > 4096)
            throw new IllegalArgumentException("Это не QR-код контакта «Близко».");
        return value; // The shared core validates the key and address before saving.
    }

    static Bitmap image(String code) throws WriterException {
        requireContact(code);
        Map<EncodeHintType,Object> hints = new EnumMap<>(EncodeHintType.class);
        hints.put(EncodeHintType.MARGIN,4);
        hints.put(EncodeHintType.ERROR_CORRECTION,ErrorCorrectionLevel.M);
        BitMatrix matrix = new QRCodeWriter().encode(code,BarcodeFormat.QR_CODE,1024,1024,hints);
        int width=matrix.getWidth(),height=matrix.getHeight();
        int[] pixels=new int[width*height];
        for(int y=0;y<height;y++)for(int x=0;x<width;x++)pixels[y*width+x]=matrix.get(x,y)?0xFF000000:0xFFFFFFFF;
        return Bitmap.createBitmap(pixels,width,height,Bitmap.Config.ARGB_8888);
    }

    static String decode(Bitmap bitmap) throws Exception {
        int width=bitmap.getWidth(),height=bitmap.getHeight();
        int[] pixels=new int[width*height];bitmap.getPixels(pixels,0,width,0,0,width,height);
        Map<DecodeHintType,Object> hints=new EnumMap<>(DecodeHintType.class);
        hints.put(DecodeHintType.TRY_HARDER,Boolean.TRUE);
        try {
            return requireContact(new QRCodeReader().decode(new BinaryBitmap(new HybridBinarizer(new RGBLuminanceSource(width,height,pixels))),hints).getText());
        } catch (ReaderException e) {
            throw new IllegalArgumentException("QR-код не найден. Выберите чёткое изображение с кодом целиком.");
        }
    }

    static String readImage(Context context,Uri uri) throws Exception {
        BitmapFactory.Options bounds=new BitmapFactory.Options();bounds.inJustDecodeBounds=true;
        try(InputStream input=context.getContentResolver().openInputStream(uri)){BitmapFactory.decodeStream(input,null,bounds);}
        if(bounds.outWidth<=0||bounds.outHeight<=0)throw new IOException("Не удалось открыть изображение.");
        BitmapFactory.Options options=new BitmapFactory.Options();
        options.inSampleSize=1;
        while(Math.max(bounds.outWidth,bounds.outHeight)/options.inSampleSize>2048)options.inSampleSize*=2;
        Bitmap bitmap;
        try(InputStream input=context.getContentResolver().openInputStream(uri)){bitmap=BitmapFactory.decodeStream(input,null,options);}
        if(bitmap==null)throw new IOException("Не удалось открыть изображение.");
        try{return decode(bitmap);}finally{bitmap.recycle();}
    }

    static Uri shareImage(Context context,Bitmap image) throws IOException {
        File directory=new File(context.getCacheDir(),"contact-qr");
        if(!directory.isDirectory()&&!directory.mkdirs())throw new IOException("Не удалось подготовить QR-код.");
        File file=new File(directory,"Blizko-contact.png");
        try(OutputStream output=new FileOutputStream(file)){
            if(!image.compress(Bitmap.CompressFormat.PNG,100,output))throw new IOException("Не удалось сохранить QR-код.");
        }
        return FileProvider.getUriForFile(context,context.getPackageName()+".qr",file);
    }
}
