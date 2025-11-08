package main

import (
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	"harmonydb"

	"github.com/charmbracelet/log"
)

const charset = "abcdefghijklmnopqrstuvwxyz" +
	"ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

var seededRand *rand.Rand = rand.New(
	rand.NewSource(time.Now().UnixNano()))

func StringWithCharset(length int, charset string) string {
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[seededRand.Intn(len(charset))]
	}
	return string(b)
}

func String(length int) string {
	return StringWithCharset(length, charset)
}

func main() {
	rand.Seed(time.Now().UnixNano())

	fmt.Printf("Starting HarmonyDB client (PID: %d)\n", os.Getpid())

	db, err := harmonydb.Open()
	if err != nil {
		fmt.Printf("Error opening database: %v\n", err)
		os.Exit(1)
	}

	// Wait for cluster to stabilize
	time.Sleep(3 * time.Second)

	// Generate random test data periodically
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	go func() {
		for {
			select {
			case <-ticker.C:
				key := fmt.Sprintf("key_%d", rand.Intn(100))
				value := fmt.Sprintf(String(10))

				log.Info("adding key")
				err := db.Put([]byte(key), []byte(value))
				if err != nil {
					fmt.Printf("Put error: %v\n", err)
					continue
				}

				log.Info("get key")
				retr, err := db.Get([]byte(key))
				if err != nil {
					fmt.Printf("get error %w", err)
					continue
				}
				fmt.Printf("Found %s=%s\n", key, string(retr))
			}
		}
	}()

	// Random get operations
	getTicker := time.NewTicker(15 * time.Second)
	defer getTicker.Stop()

	go func() {
		for {
			select {
			case <-getTicker.C:
			}
		}
	}()

	// Set up graceful shutdown
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	fmt.Printf("HarmonyDB client is running... Press Ctrl+C to stop\n")

	// Wait for shutdown signal
	<-c
	fmt.Printf("\nShutting down gracefully...\n")

	fmt.Printf("Shutdown complete\n")
}
