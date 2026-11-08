package gateway

import (
	"errors"
	"fmt"
	"unicode/utf8"

	"github.com/kmpoltorak/routetus/internal/config"
)

// Bound the compiled report once, before it reaches HTTP, status, or logging.
func validationReport(err error) error {
	const maxFields, maxMessage, maxBytes = 100, 256, 8 << 10
	fields := config.FieldErrors(err)
	if len(fields) == 0 {
		return err
	}
	reported := make([]error, 0, min(len(fields), maxFields)+1)
	size := 0
	for _, f := range fields {
		if len(reported) == maxFields {
			break
		}
		if len(f.Message) > maxMessage {
			end := maxMessage - len("...")
			for !utf8.RuneStart(f.Message[end]) {
				end--
			}
			f.Message = f.Message[:end] + "..."
		}
		next := len(f.Error()) + 1
		// Reserve room for the omission entry and the runtime rejection prefix.
		if size+next > maxBytes-128 {
			break
		}
		reported = append(reported, f)
		size += next
	}
	if omitted := len(fields) - len(reported); omitted > 0 {
		reported = append(reported, config.Invalid("/", fmt.Sprintf("%d more errors omitted", omitted)))
	}
	return errors.Join(reported...)
}
