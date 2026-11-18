package app

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/smtp"
	"strings"
	"testing"
	"time"
)

func TestSMTPRelayDeliveryResultUsesDataAcknowledgment(t *testing.T) {
	for _, accepted := range []bool{true, false} {
		t.Run(fmt.Sprintf("accepted=%t", accepted), func(t *testing.T) {
			conn, server := net.Pipe()
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
			done := make(chan struct{})
			go func() {
				defer close(done)
				defer server.Close()
				_ = server.SetDeadline(time.Now().Add(5 * time.Second))
				reader := bufio.NewReader(server)
				_, _ = io.WriteString(server, "220 relay.test ESMTP\r\n")
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					switch {
					case strings.HasPrefix(line, "DATA"):
						_, _ = io.WriteString(server, "354 Send message\r\n")
						for {
							line, err = reader.ReadString('\n')
							if err != nil {
								return
							}
							if line == ".\r\n" {
								break
							}
						}
						if accepted {
							_, _ = io.WriteString(server, "250 Message queued\r\n")
						} else {
							_, _ = io.WriteString(server, "451 Temporary delivery failure\r\n")
						}
					case strings.HasPrefix(line, "QUIT"):
						// Drop the connection without acknowledging QUIT.
						return
					default:
						_, _ = io.WriteString(server, "250 OK\r\n")
					}
				}
			}()
			client, err := smtp.NewClient(conn, "relay.test")
			if err != nil {
				t.Fatal(err)
			}
			err = sendSMTPMessage(client, nil, "sender@example.com", []string{"recipient@example.com"}, []byte("Subject: test\r\n\r\nmessage\r\n"))
			_ = client.Close()
			<-done
			if accepted && err != nil {
				t.Fatalf("accepted message incorrectly marked for retry: %v", err)
			}
			if !accepted && err == nil {
				t.Fatal("rejected message incorrectly marked delivered")
			}
		})
	}
}
