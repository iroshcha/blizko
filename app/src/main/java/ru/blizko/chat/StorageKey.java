package ru.blizko.chat;

import android.content.Context;
import android.security.keystore.KeyGenParameterSpec;
import android.security.keystore.KeyProperties;
import android.util.AtomicFile;
import java.io.*;
import java.security.*;
import javax.crypto.*;
import javax.crypto.spec.GCMParameterSpec;

final class StorageKey {
    static byte[] load(Context context) throws Exception {
        AtomicFile file = new AtomicFile(new File(context.getNoBackupFilesDir(), "master.wrapped"));
        KeyStore ks = KeyStore.getInstance("AndroidKeyStore"); ks.load(null);
        String alias = "blizko-master-wrap-v2";
        boolean exists = file.getBaseFile().exists();
        if (!ks.containsAlias(alias)) {
            if (exists) throw new GeneralSecurityException("Ключ хранилища недоступен");
            KeyGenerator gen = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore");
            gen.init(new KeyGenParameterSpec.Builder(alias, KeyProperties.PURPOSE_ENCRYPT | KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM).setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE).setKeySize(256).build());
            gen.generateKey();
        }
        Cipher cipher = Cipher.getInstance("AES/GCM/NoPadding");
        if (exists) {
            byte[] data = file.readFully();
            cipher.init(Cipher.DECRYPT_MODE, ks.getKey(alias,null), new GCMParameterSpec(128,data,0,12));
            return cipher.doFinal(data,12,data.length-12);
        }
        if (new File(context.getNoBackupFilesDir(), "core/vault").exists()) throw new IOException("Ключ хранилища утрачен");
        byte[] key = new byte[32]; new SecureRandom().nextBytes(key);
        cipher.init(Cipher.ENCRYPT_MODE,ks.getKey(alias,null)); byte[] encrypted=cipher.doFinal(key);
        FileOutputStream stream = null;
        try { stream=file.startWrite();stream.write(cipher.getIV());stream.write(encrypted);file.finishWrite(stream); }
        catch(Exception e){if(stream!=null)file.failWrite(stream);throw e;}
        return key;
    }
}
