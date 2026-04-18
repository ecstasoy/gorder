package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"
)

const (
	email     = "huang.kunh@northeastern.edu"
	gistURL   = "https://gist.github.com/ecstasoy/f92ef7885bad472f1c70bdf7d59906ee"
	submitURL = "https://api.challenge.hennge.com/challenges/backend-recursion/004"
)

func generateTOTP(secret string) string {
	t := time.Now().Unix() / 30

	msg := make([]byte, 8)
	binary.BigEndian.PutUint64(msg, uint64(t))

	mac := hmac.New(sha512.New, []byte(secret))
	mac.Write(msg)
	h := mac.Sum(nil)

	offset := h[len(h)-1] & 0x0f
	binCode := binary.BigEndian.Uint32(h[offset:offset+4]) & 0x7fffffff

	otp := binCode % uint32(math.Pow10(10))
	return fmt.Sprintf("%010d", otp)
}

func main() {
	secret := email + "HENNGECHALLENGE004"
	totp := generateTOTP(secret)

	body, _ := json.Marshal(map[string]string{
		"github_url":        gistURL,
		"contact_email":     email,
		"solution_language": "golang",
	})

	req, _ := http.NewRequest("POST", submitURL, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.SetBasicAuth(email, totp)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	fmt.Println("Status:", resp.Status)
	fmt.Println("Response:", string(respBody))
}
