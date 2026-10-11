package transport

// ControlAddress is not a device public key. TLS-authenticated server replies to
// this address may manage storage/preferences but can never acknowledge delivery.
const ControlAddress = "0000000000000000000000000000000000000000000000000000000000000000"

type Offered struct {
	Frame   Frame
	Stored  bool
	Expires int64
}
type DurableStore interface {
	Offer(Frame) (Offered, error)
	SaveReceipt(Frame) error
	Pending(address string, limit int) []Frame
	Control(Frame) Response
}
