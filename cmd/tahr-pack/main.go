package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"tahr/internal/core/plugin"
)

func main() {
	pluginsDir := flag.String("dir", "plugins", "Path to plugins root directory")
	onlyPlugin := flag.String("plugin", "", "Optional: only pack a specific plugin name")
	flag.Parse()

	entries, err := os.ReadDir(*pluginsDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading plugins directory %s: %v\n", *pluginsDir, err)
		os.Exit(1)
	}

	fmt.Println("==================================================")
	fmt.Printf(" Building Tahr Plugin Archives (.tahr) from: %s\n", *pluginsDir)
	fmt.Println(" Compression: Zstandard (Method 93, Best Compression)")
	fmt.Println("==================================================")

	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if *onlyPlugin != "" && e.Name() != *onlyPlugin {
			continue
		}
		pDir := filepath.Join(*pluginsDir, e.Name())
		manifestPath := filepath.Join(pDir, "plugin.json")
		m, err := plugin.LoadManifest(manifestPath)
		if err != nil {
			continue
		}
		if *onlyPlugin != "" && m.ID != *onlyPlugin && e.Name() != *onlyPlugin {
			continue
		}

		destArchive := filepath.Join(*pluginsDir, fmt.Sprintf("%s.tahr", m.ID))
		if err := plugin.PackDirectory(pDir, destArchive); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to pack %s: %v\n", m.ID, err)
			continue
		}

		// Verify archive integrity by dry unpacking
		tempDir, err := os.MkdirTemp("", "tahr-verify-*")
		if err == nil {
			vManifest, vErr := plugin.UnpackArchive(destArchive, tempDir)
			_ = os.RemoveAll(tempDir)
			if vErr != nil || vManifest.ID != m.ID {
				fmt.Fprintf(os.Stderr, "Verification failed for %s: %v\n", destArchive, vErr)
				continue
			}
		}

		fi, _ := os.Stat(destArchive)
		size := int64(0)
		if fi != nil {
			size = fi.Size()
		}

		fmt.Printf("[%-16s] v%-5s -> %s (%d bytes)\n", m.ID, m.Version, filepath.Base(destArchive), size)
		count++
	}

	fmt.Println("--------------------------------------------------")
	fmt.Printf("Successfully packaged and verified %d plugins.\n", count)
}
