package mobile

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// An authenticated append-only inventory detects lost records without rewriting
// the entire history on every send. A durable intent recovers a interrupted write.
type recordHead struct {
	Seq  int64  `json:"seq"`
	Hash string `json:"hash"`
}
type recordEvent struct {
	Seq    int64  `json:"seq"`
	Prev   string `json:"prev"`
	ID     string `json:"id"`
	Digest string `json:"digest"`
}
type recordIntent struct {
	Base    recordHead `json:"base"`
	Message message    `json:"message"`
}
type recordIntegrity struct {
	head    recordHead
	entries map[string]string
	failed  bool
}
type journalRow struct {
	event recordEvent
	head  recordHead
	end   int64
}

func messageDigest(m message) string {
	raw, _ := json.Marshal(m)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (v *vault) journalFrame(event recordEvent) ([]byte, error) {
	raw, err := json.Marshal(event)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, v.aead.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	sealed := v.aead.Seal(nonce, nonce, raw, []byte("blizko-inventory/5"))
	frame := make([]byte, 4, len(sealed)+4)
	binary.LittleEndian.PutUint32(frame, uint32(len(sealed)))
	return append(frame, sealed...), nil
}
func frameHead(seq int64, frame []byte) recordHead {
	sum := sha256.Sum256(frame)
	return recordHead{seq, hex.EncodeToString(sum[:])}
}
func (v *vault) readJournal() ([]journalRow, error) {
	file, err := os.Open(filepath.Join(v.dir, "records.log"))
	if err != nil {
		return nil, err
	}
	defer file.Close()
	rows := []journalRow{}
	head := recordHead{}
	var offset int64
	for {
		length := make([]byte, 4)
		_, err = io.ReadFull(file, length)
		if err == io.EOF {
			return rows, nil
		}
		if err != nil {
			return rows, err
		}
		size := binary.LittleEndian.Uint32(length)
		if size < uint32(v.aead.NonceSize()+v.aead.Overhead()) || size > 65536 {
			return rows, errors.New("invalid inventory frame")
		}
		sealed := make([]byte, size)
		if _, err = io.ReadFull(file, sealed); err != nil {
			return rows, err
		}
		raw, err := v.aead.Open(nil, sealed[:v.aead.NonceSize()], sealed[v.aead.NonceSize():], []byte("blizko-inventory/5"))
		if err != nil {
			return rows, err
		}
		var event recordEvent
		if err = json.Unmarshal(raw, &event); err != nil || event.Seq != head.Seq+1 || event.Prev != head.Hash || len(event.ID) != 64 || len(event.Digest) != 64 {
			return rows, errors.New("damaged inventory")
		}
		head = frameHead(event.Seq, append(length, sealed...))
		offset += int64(4 + size)
		rows = append(rows, journalRow{event, head, offset})
	}
}
func (v *vault) migrateIntegrity(messages []message) error {
	file, err := os.CreateTemp(v.dir, ".encrypted-inventory-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	head := recordHead{}
	entries := map[string]string{}
	for _, m := range messages {
		id, digest := recordID(m), messageDigest(m)
		if _, exists := entries[id]; exists {
			file.Close()
			return errors.New("duplicate record")
		}
		frame, e := v.journalFrame(recordEvent{head.Seq + 1, head.Hash, id, digest})
		if e != nil {
			file.Close()
			return e
		}
		if _, e = file.Write(frame); e != nil {
			file.Close()
			return e
		}
		head = frameHead(head.Seq+1, frame)
		entries[id] = digest
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = replaceVaultFile(file.Name(), filepath.Join(v.dir, "records.log")); err != nil {
		return err
	}
	raw, _ := json.Marshal(head)
	if err = v.write("records-head", raw); err != nil {
		return err
	}
	v.integrity = &recordIntegrity{head: head, entries: entries}
	return nil
}
func (v *vault) finishRecordIntent(intent recordIntent, alreadyAppended bool) error {
	fault := func(stage string) error {
		if v.recordFault != nil {
			return v.recordFault(stage)
		}
		return nil
	}
	if err := fault("intent"); err != nil {
		return err
	}
	if err := v.writeMessageRaw(intent.Message); err != nil {
		return err
	}
	if err := fault("record"); err != nil {
		return err
	}
	next := v.integrity.head
	if !alreadyAppended {
		event := recordEvent{intent.Base.Seq + 1, intent.Base.Hash, recordID(intent.Message), messageDigest(intent.Message)}
		frame, err := v.journalFrame(event)
		if err != nil {
			return err
		}
		file, err := os.OpenFile(filepath.Join(v.dir, "records.log"), os.O_WRONLY|os.O_APPEND, 0600)
		if err != nil {
			return err
		}
		_, err = file.Write(frame)
		if err == nil {
			err = file.Sync()
		}
		closed := file.Close()
		if err != nil {
			return err
		}
		if closed != nil {
			return closed
		}
		next = frameHead(event.Seq, frame)
	}
	if err := fault("journal"); err != nil {
		return err
	}
	raw, _ := json.Marshal(next)
	if err := v.write("records-head", raw); err != nil {
		return err
	}
	if err := fault("head"); err != nil {
		return err
	}
	v.integrity.head = next
	v.integrity.entries[recordID(intent.Message)] = messageDigest(intent.Message)
	return v.write("record-pending", nil)
}
func (v *vault) prepareIntegrity() error {
	raw, err := v.read("records-head")
	if err != nil {
		return err
	}
	var head recordHead
	if err = json.Unmarshal(raw, &head); err != nil || head.Seq < 0 {
		return errors.New("invalid inventory head")
	}
	pendingRaw, pendingErr := v.read("record-pending")
	if pendingErr != nil && !os.IsNotExist(pendingErr) {
		return pendingErr
	}
	var pending *recordIntent
	if pendingErr == nil {
		pending = &recordIntent{}
		if json.Unmarshal(pendingRaw, pending) != nil || pending.Message.Order <= 0 {
			return errors.New("invalid storage intent")
		}
	}
	rows, scanErr := v.readJournal()
	if int64(len(rows)) < head.Seq {
		return errors.New("missing inventory entries")
	}
	at := recordHead{}
	var end int64
	if head.Seq > 0 {
		at = rows[head.Seq-1].head
		end = rows[head.Seq-1].end
	}
	if at != head {
		return errors.New("inventory head mismatch")
	}
	entries := map[string]string{}
	for _, row := range rows[:head.Seq] {
		entries[row.event.ID] = row.event.Digest
	}
	v.integrity = &recordIntegrity{head: head, entries: entries}
	if pending == nil {
		if scanErr != nil || int64(len(rows)) != head.Seq {
			return errors.New("unexpected inventory tail")
		}
		return nil
	}
	digest, id := messageDigest(pending.Message), recordID(pending.Message)
	already := false
	switch {
	case head == pending.Base:
		if int64(len(rows)) == head.Seq+1 && scanErr == nil {
			row := rows[head.Seq]
			if row.event.ID != id || row.event.Digest != digest {
				return errors.New("inventory intent mismatch")
			}
			v.integrity.head = row.head
			already = true
		} else if int64(len(rows)) == head.Seq {
			// Only a torn, uncommitted tail with a matching durable intent can be discarded.
			if scanErr != nil {
				file, e := os.OpenFile(filepath.Join(v.dir, "records.log"), os.O_WRONLY, 0600)
				if e != nil {
					return e
				}
				e = file.Truncate(end)
				if e == nil {
					e = file.Sync()
				}
				file.Close()
				if e != nil {
					return e
				}
			}
		} else {
			return errors.New("unexpected recovery tail")
		}
	case head.Seq == pending.Base.Seq+1 && head.Seq > 0 && scanErr == nil && int64(len(rows)) == head.Seq:
		row := rows[head.Seq-1]
		if row.event.Prev != pending.Base.Hash || row.event.ID != id || row.event.Digest != digest {
			return errors.New("committed intent mismatch")
		}
		already = true
	default:
		return errors.New("inventory recovery mismatch")
	}
	return v.finishRecordIntent(*pending, already)
}
func (v *vault) validateInventory(messages []message) error {
	if len(messages) != len(v.integrity.entries) {
		return errors.New("missing history records")
	}
	seen := map[string]bool{}
	for _, m := range messages {
		id := recordID(m)
		if seen[id] || v.integrity.entries[id] != messageDigest(m) {
			return errors.New("history inventory mismatch")
		}
		seen[id] = true
	}
	return nil
}
