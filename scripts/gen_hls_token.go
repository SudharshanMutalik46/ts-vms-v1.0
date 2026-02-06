package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 4 {
		fmt.Println("Usage: go run gen_hls_token.go <sub/camera_id> <sid/session_id> <exp>")
		return
	}
	sub := os.Args[1]
	sid := os.Args[2]
	exp := os.Args[3]
	key := []byte("dev-hls-secret")

	canonical := fmt.Sprintf("hls|%s|%s|%s", sub, sid, exp)
	h := hmac.New(sha256.New, key)
	h.Write([]byte(canonical))
	sig := hex.EncodeToString(h.Sum(nil))

	fmt.Printf("token_params: sub=%s&sid=%s&exp=%s&scope=hls&kid=v1&sig=%s\n", sub, sid, exp, sig)
}
