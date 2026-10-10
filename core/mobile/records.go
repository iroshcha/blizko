package mobile

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Each message is an independently authenticated atomic record. Metadata is
// committed last during migration, so the legacy file remains a recovery source.
func recordID(m message) string {
	sum := sha256.Sum256([]byte(m.Peer + ":" + m.ID + ":" + map[bool]string{true: "out", false: "in"}[m.Out]))
	return hex.EncodeToString(sum[:])
}

func (v *vault) writeMessageRaw(m message) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	dir := filepath.Join(v.dir, "messages")
	if err := os.Mkdir(dir, 0700); err != nil {
		if !os.IsExist(err) {
			return err
		}
	} else {
		if err := syncVaultDirectory(dir); err != nil {
			return err
		}
	}
	name := recordID(m)
	raw, err := json.Marshal(m)
	if err != nil {
		return err
	}
	nonce := make([]byte, v.aead.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}
	encrypted := v.aead.Seal(nonce, nonce, raw, []byte("blizko-record/4/"+name))
	file, err := os.CreateTemp(dir, ".encrypted-")
	if err != nil {
		return err
	}
	tmp := file.Name()
	defer os.Remove(tmp)
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(encrypted)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return replaceVaultFile(tmp, filepath.Join(dir, name+".enc"))
}

func (v *vault) writeMessage(m message) error {
	v.recordMu.Lock()
	defer v.recordMu.Unlock()
	if v.integrity == nil {
		return v.writeMessageRaw(m)
	}
	if v.integrity.failed {
		return errors.New("Хранилище требует повторного открытия. Данные не удалены.")
	}
	pending := recordIntent{Base: v.integrity.head, Message: m}
	raw, err := json.Marshal(pending)
	if err != nil {
		return err
	}
	if err = v.write("record-pending", raw); err != nil {
		v.integrity.failed = true
		return err
	}
	err = v.finishRecordIntent(pending, false)
	if err != nil {
		v.integrity.failed = true
	}
	return err
}

func (v *vault) readMessages() ([]message, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	dir := filepath.Join(v.dir, "messages")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []message{}, nil
	}
	if err != nil {
		return nil, err
	}
	messages := make([]message, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".encrypted-") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".enc")
		if len(name) != 64 || entry.Name() != name+".enc" {
			return nil, errors.New("unknown storage record")
		}
		if _, err = hex.DecodeString(name); err != nil {
			return nil, err
		}
		path := filepath.Join(dir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if info.Size() > 64000 {
			return nil, errors.New("storage record too large")
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		if len(raw) < v.aead.NonceSize() {
			return nil, errors.New("damaged storage record")
		}
		plain, err := v.aead.Open(nil, raw[:v.aead.NonceSize()], raw[v.aead.NonceSize():], []byte("blizko-record/4/"+name))
		if err != nil {
			return nil, err
		}
		var m message
		if err = json.Unmarshal(plain, &m); err != nil {
			return nil, err
		}
		if recordID(m) != name || m.Order <= 0 {
			return nil, errors.New("invalid storage record")
		}
		messages = append(messages, m)
	}
	sort.Slice(messages, func(i, j int) bool { return messages[i].Order < messages[j].Order })
	return messages, nil
}

func (n *Node) rebuildIndexes() {
	n.messageIndexes = make(map[string]int, len(n.state.Messages))
	n.historyIndexes = map[string][]int{}
	n.visibleIndexes = nil
	n.inboxIndexes = map[string][]int{}
	n.previews = map[string]string{}
	n.incoming = 0
	n.nextOrder = 0
	for i, m := range n.state.Messages {
		n.messageIndexes[recordID(m)] = i
		if !m.Archived {
			n.visibleIndexes = append(n.visibleIndexes, i)
			n.historyIndexes[m.Peer] = append(n.historyIndexes[m.Peer], i)
			n.previews[m.Peer] = m.Text
			if !m.Out {
				n.inboxIndexes[m.Peer] = append(n.inboxIndexes[m.Peer], i)
			}
		}
		if m.Order > n.nextOrder {
			n.nextOrder = m.Order
		}
		if !m.Out {
			n.incoming++
		}
	}
	n.revision++
}

func (n *Node) storeMessage(m message, index int) error {
	if m.Order == 0 {
		m.Order = n.nextOrder + 1
	}
	if err := n.storage.writeMessage(m); err != nil {
		return err
	}
	if index < 0 {
		n.state.Messages = append(n.state.Messages, m)
		n.messageIndexes[recordID(m)] = len(n.state.Messages) - 1
		if !m.Archived {
			n.visibleIndexes = append(n.visibleIndexes, len(n.state.Messages)-1)
			n.historyIndexes[m.Peer] = append(n.historyIndexes[m.Peer], len(n.state.Messages)-1)
			n.previews[m.Peer] = m.Text
			if !m.Out {
				n.inboxIndexes[m.Peer] = append(n.inboxIndexes[m.Peer], len(n.state.Messages)-1)
			}
		}
		if !m.Out {
			n.incoming++
		}
	} else {
		n.state.Messages[index] = m
	}
	if m.Order > n.nextOrder {
		n.nextOrder = m.Order
	}
	n.revision++
	return nil
}

// Clear only displayed history; durable tombstones retain duplicate detection.
// Pending outbound messages and device/contact keys are deliberately retained.
func (n *Node) ClearHistory(peer string) error {
	return n.clearHistory(context.Background(), peer, nil)
}
func (n *Node) clearHistory(ctx context.Context, peer string, progress func(int, int)) error {
	n.mu.Lock()
	if _, ok := n.peer(peer); !ok {
		n.mu.Unlock()
		return errors.New("Контакт не найден")
	}
	indices := []int{}
	for i, m := range n.state.Messages {
		if m.Peer != peer || m.Archived || (m.Out && !m.Delivered && !m.Cancelled) {
			continue
		}
		indices = append(indices, i)
	}
	n.mu.Unlock()
	if progress != nil {
		progress(0, len(indices))
	}
	// Permit delivery and status reads between atomic record writes.
	defer func() { n.mu.Lock(); n.rebuildIndexes(); n.mu.Unlock() }()
	for done, i := range indices {
		if err := ctx.Err(); err != nil {
			return err
		}
		n.mu.Lock()
		m := n.state.Messages[i]
		if m.Archived {
			n.mu.Unlock()
			continue
		}
		m.Archived = true
		m.Text = ""
		m.Packet = nil
		err := n.storeMessage(m, i)
		n.mu.Unlock()
		if err != nil {
			return err
		}
		if progress != nil {
			progress(done+1, len(indices))
		}
	}
	return nil
}
