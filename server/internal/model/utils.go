// Package model stores what the gateway remembers.
// OUT OF DATE: this whole file is left over from the old pairing design
// (RSA signatures and gateway-generated UUIDs). Nothing in the project uses it anymore.
package model

// OUT OF DATE: these imports are only needed by the code below.
// Go refuses to build with unused imports, so they are commented out too.
// import (
// 	"crypto"
// 	"crypto/rsa"
// 	"crypto/sha256"
// 	"crypto/x509"
// 	"encoding/binary"
// 	"encoding/pem"
// 	"errors"
// 	"fmt"
//
// 	"github.com/google/uuid"
// )

// OUT OF DATE: VerifySignature checked a sensor's RSA signature on its data.
// Sensors no longer sign their data, so this is never called.
// func VerifySignature(data []byte, signature []byte, publicKey *rsa.PublicKey) bool {
// 	hash := sha256.Sum256(data)
// 	err := rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, hash[:], signature)
// 	return err == nil
// }

// OUT OF DATE: ParsePublicKey read a sensor's RSA public key sent during pairing.
// Pairing no longer uses keys, so this is never called.
// func ParsePublicKey(value []byte) (*rsa.PublicKey, error) {
// 	block, _ := pem.Decode(value)
// 	if block == nil {
// 		return nil, errors.New("failed to parse public key")
// 	}
//
// 	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
// 	if err != nil {
// 		return nil, err
// 	}
//
// 	rsaPub, ok := pub.(*rsa.PublicKey)
// 	if !ok {
// 		return nil, errors.New("failed to parse public key")
// 	}
//
// 	return rsaPub, nil
// }

// OUT OF DATE: UuidToBytes turned a UUID into 16 bytes to send to a sensor.
// Never called.
// func UuidToBytes(uuid [4]uint32) []byte {
// 	result := []byte{}
// 	result = binary.LittleEndian.AppendUint32(result, uuid[0])
// 	result = binary.LittleEndian.AppendUint32(result, uuid[1])
// 	result = binary.LittleEndian.AppendUint32(result, uuid[2])
// 	result = binary.LittleEndian.AppendUint32(result, uuid[3])
// 	return result
// }

// OUT OF DATE: BytesToUuid turned 16 bytes back into a UUID.
// Only used by GenerateUUID below.
// func BytesToUuid(value [16]byte) [4]uint32 {
// 	return [4]uint32{
// 		binary.LittleEndian.Uint32(value[0:4]),
// 		binary.LittleEndian.Uint32(value[4:8]),
// 		binary.LittleEndian.Uint32(value[8:12]),
// 		binary.LittleEndian.Uint32(value[12:16]),
// 	}
// }

// OUT OF DATE: UuidToString printed a UUID as text, like "1ea8c363-a161-...".
// Never called.
// func UuidToString(uuid [4]uint32) string {
// 	return fmt.Sprintf("%08x-%04x-%04x-%04x-%04x%08x", uuid[3], uuid[2]>>16, uuid[2]&0xffff, uuid[1]>>16, uuid[1]&0xffff, uuid[0])
// }

// OUT OF DATE: GenerateUUID made a random UUID for the gateway's old Bluetooth slots.
// Only used by GetDataCharUUID and GetSettingsCharUUID in gateway.go, which are commented out.
// func GenerateUUID() ([4]uint32, error) {
// 	u, err := uuid.New().MarshalBinary()
// 	if len(u) != 16 || err != nil {
// 		return [4]uint32{}, errors.New("failed to generate UUID")
// 	}
// 	return BytesToUuid([16]byte(u)), nil
// }