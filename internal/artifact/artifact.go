package artifact

import (
	"encoding/hex"
	"errors"
	"fmt"

	"azper/internal/domain"

	"github.com/zeebo/blake3"
)

func FromBytes(data []byte, mediaType string) (domain.Artifact, error) {
	digestBytes := blake3.Sum256(data)
	digest := hex.EncodeToString(digestBytes[:])
	artifact := domain.Artifact{
		ID:        "art_b3_" + digest,
		Algorithm: "blake3-256",
		Digest:    digest,
		MediaType: mediaType,
		Size:      int64(len(data)),
		Data:      append([]byte(nil), data...),
	}
	if err := artifact.Validate(); err != nil {
		return domain.Artifact{}, fmt.Errorf("build content-addressed artifact: %w", err)
	}
	return artifact, nil
}

func Verify(candidate domain.Artifact) error {
	if err := candidate.Validate(); err != nil {
		return err
	}
	expected, err := FromBytes(candidate.Data, candidate.MediaType)
	if err != nil {
		return err
	}
	if candidate.ID != expected.ID || candidate.Algorithm != expected.Algorithm || candidate.Digest != expected.Digest || candidate.Size != expected.Size {
		return errors.New("artifact content does not match its BLAKE3-256 address")
	}
	return nil
}
