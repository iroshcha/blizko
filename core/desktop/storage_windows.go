//go:build windows

package main

import (
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows protects this random key silently; the app has no sign-in or
// registration. It never sends the storage key across the UI process boundary.
func protectKey(value []byte, decrypt bool) ([]byte, error) {
	if len(value) == 0 {
		return nil, errors.New("Пустой ключ хранилища")
	}
	input := windows.DataBlob{Size: uint32(len(value)), Data: &value[0]}
	var output windows.DataBlob
	var err error
	if decrypt {
		err = windows.CryptUnprotectData(&input, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &output)
	} else {
		err = windows.CryptProtectData(&input, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &output)
	}
	runtime.KeepAlive(value)
	if err != nil {
		return nil, errors.New("Не удалось открыть ключ хранилища. Данные сохранены.")
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(output.Data)))
	return append([]byte(nil), unsafe.Slice(output.Data, output.Size)...), nil
}

func storageKey(dir string) ([]byte, error) {
	file := filepath.Join(dir, "master.wrapped")
	wrapped, err := os.ReadFile(file)
	if err == nil {
		key, err := protectKey(wrapped, true)
		if err != nil {
			return nil, err
		}
		if len(key) != 32 {
			return nil, errors.New("Ключ хранилища повреждён. Данные сохранены.")
		}
		return key, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(dir, "core", "vault")); !os.IsNotExist(err) {
		return nil, errors.New("Ключ хранилища утрачен. История не удалена.")
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	wrapped, err = protectKey(key, false)
	if err != nil {
		return nil, err
	}
	// The engine holds an exclusive directory lock before creating this file.
	if err := os.WriteFile(file, wrapped, 0600); err != nil {
		return nil, err
	}
	return key, nil
}

func lockStorage(dir string) (func(), error) {
	name, err := windows.UTF16PtrFromString(filepath.Join(dir, ".engine.lock"))
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_ALWAYS, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, errors.New("Эта история уже открыта другим окном «Близко».")
	}
	return func() { _ = windows.CloseHandle(handle) }, nil
}
