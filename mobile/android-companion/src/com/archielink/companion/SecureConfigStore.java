package com.archielink.companion;

import android.content.Context;
import android.content.SharedPreferences;
import android.security.keystore.KeyGenParameterSpec;
import android.security.keystore.KeyProperties;

import java.nio.charset.StandardCharsets;
import java.security.KeyStore;
import java.util.Base64;

import javax.crypto.Cipher;
import javax.crypto.KeyGenerator;
import javax.crypto.SecretKey;
import javax.crypto.spec.GCMParameterSpec;

final class SecureConfigStore {
    private static final String KEY_ALIAS = "archielink-config-storage-v1";
    private static final String PREFS = "secure_config";
    private static final String VALUE = "value";

    static boolean has(Context context) {
        return context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
            .contains(VALUE);
    }

    static void save(Context context, String config) throws Exception {
        Cipher cipher = Cipher.getInstance("AES/GCM/NoPadding");
        cipher.init(Cipher.ENCRYPT_MODE, key());
        byte[] ciphertext = cipher.doFinal(config.getBytes(StandardCharsets.UTF_8));
        byte[] nonce = cipher.getIV();
        byte[] envelope = new byte[nonce.length + ciphertext.length];
        System.arraycopy(nonce, 0, envelope, 0, nonce.length);
        System.arraycopy(ciphertext, 0, envelope, nonce.length, ciphertext.length);
        SharedPreferences preferences = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE);
        preferences.edit()
            .putString(VALUE, Base64.getEncoder().encodeToString(envelope))
            .commit();
    }

    static String load(Context context) {
        String value = context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
            .getString(VALUE, "");
        if (value.isEmpty()) {
            return null;
        }
        try {
            byte[] envelope = Base64.getDecoder().decode(value);
            if (envelope.length <= 12) {
                throw new IllegalArgumentException("encrypted config is truncated");
            }
            byte[] nonce = new byte[12];
            byte[] ciphertext = new byte[envelope.length - nonce.length];
            System.arraycopy(envelope, 0, nonce, 0, nonce.length);
            System.arraycopy(envelope, nonce.length, ciphertext, 0, ciphertext.length);
            Cipher cipher = Cipher.getInstance("AES/GCM/NoPadding");
            cipher.init(Cipher.DECRYPT_MODE, key(), new GCMParameterSpec(128, nonce));
            return new String(cipher.doFinal(ciphertext), StandardCharsets.UTF_8);
        } catch (Exception error) {
            clear(context);
            return null;
        }
    }

    static void clear(Context context) {
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)
            .edit()
            .remove(VALUE)
            .commit();
    }

    private static SecretKey key() throws Exception {
        KeyStore store = KeyStore.getInstance("AndroidKeyStore");
        store.load(null);
        if (store.containsAlias(KEY_ALIAS)) {
            return (SecretKey) store.getKey(KEY_ALIAS, null);
        }
        KeyGenerator generator = KeyGenerator.getInstance(
            KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore"
        );
        generator.init(new KeyGenParameterSpec.Builder(
            KEY_ALIAS,
            KeyProperties.PURPOSE_ENCRYPT | KeyProperties.PURPOSE_DECRYPT
        )
            .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
            .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
            .setKeySize(256)
            .build());
        return generator.generateKey();
    }
}
