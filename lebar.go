package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/Masterminds/sprig/v3"

	"github.com/goccy/go-yaml"
	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
	"github.com/traefik/yaegi/stdlib/syscall"
	"github.com/traefik/yaegi/stdlib/unrestricted"
	"github.com/traefik/yaegi/stdlib/unsafe"
)

// Config represents the tool's configuration
type Config struct {
	Separator string  `yaml:"separator"`
	Blocks    []Block `yaml:"blocks"`
}

// Block defines a status bar module
type Block struct {
	Name        string `yaml:"name"`
	Interval    int    `yaml:"interval"`
	Interpreter string `yaml:"interpreter"`
	Script      string `yaml:"script"`
	Format      string `yaml:"format"`
}

// Output represents a bar item
type Output struct {
	Name                string `json:"name"`
	FullText            string `json:"full_text"`
	Separator           bool   `json:"separator"`
	SeparatorBlockWidth int    `json:"separator_block_width,omitempty"`
}

// Default symbols list
const defaultSymbols = "🟦🟩🟨🟫🟥"
const defaultOver100Symbols = "⚠️ 💥 🆘"

// executeYaegi runs a Go script using Yaegi with full initialization
func executeYaegi(script string) (string, error) {
	i := interp.New(interp.Options{
		GoPath:       os.Getenv("GOPATH"),
		Env:          os.Environ(),
		Unrestricted: true,
	})

	// Standard library symbols
	if err := i.Use(stdlib.Symbols); err != nil {
		return "", err
	}

	// Additional symbol sets
	if err := i.Use(interp.Symbols); err != nil {
		return "", err
	}

	// Optional symbol sets (configurable)
	if err := i.Use(syscall.Symbols); err != nil {
		return "", err
	}

	if err := i.Use(unsafe.Symbols); err != nil {
		return "", err
	}

	if err := i.Use(unrestricted.Symbols); err != nil {
		return "", err
	}

	// Evaluate the script
	result, err := i.Eval(script)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%v", result.Interface()), nil
}

// executeBlock runs a block's script using the specified interpreter
func executeBlock(ctx context.Context, block Block) string {
	// Check if the interpreter is specified
	if len(block.Interpreter) < 1 {
		return "Error: Interpreter not specified"
	}

	// Split the interpreter string into the interpreter and its arguments
	parts := strings.Fields(block.Interpreter)

	// Ensure there is at least one part (the interpreter)
	if len(parts) == 0 {
		return "Error: Interpreter format is invalid"
	}

	interpreter := parts[0]                  // First part is the interpreter
	args := append([]string{}, parts[1:]...) // Remaining parts are the arguments

	// Check if the interpreter exists
	interpreterPath, err := exec.LookPath(interpreter)
	if err != nil || interpreterPath == "" {
		return fmt.Sprintf("Error: Interpreter '%s' does not exist", interpreter)
	}

	// Handle interpreter execution
	var output string
	var cmd *exec.Cmd

	if block.Interpreter == "yaegi" {
		output, err = executeYaegi(block.Script)
	} else {
		if len(args) > 0 {
			// Interpreter with args
			cmd = exec.CommandContext(ctx, interpreter, append(args, block.Script)...)
		} else {
			// Interpreter without args
			cmd = exec.CommandContext(ctx, interpreter, block.Script)
		}
	}

	if cmd != nil {
		cmdOutput, err := cmd.Output()
		if err != nil {
			return fmt.Sprintf("Error: %v", err)
		}
		output = string(cmdOutput)
	}

	text := strings.TrimSpace(output)
	if block.Format != "" {
		tmpl, err := template.New("format").Funcs(template.FuncMap{
			"Symbol": func(text string) string {
				// Extract the percentage from the text
				parts := strings.Split(text, " ")
				percentageStr := parts[len(parts)-1]
				percentageStr = strings.TrimSuffix(percentageStr, "%")
				percentage, err := strconv.ParseFloat(percentageStr, 64)
				if err != nil {
					return "?"
				}
				if percentage <= 100 {
					symbolList := strings.Split(defaultSymbols, "")
					numSymbols := len(symbolList)
					index := int((percentage / 100) * float64(numSymbols-1))
					return symbolList[index]
				} else {
					symbolList := strings.Split(defaultOver100Symbols, "")
					numSymbols := len(symbolList)
					index := int((percentage / 100) * float64(numSymbols-1))
					return symbolList[index]
				}
			},
		}).Funcs(sprig.TxtFuncMap()).Parse(block.Format)
		if err != nil {
			return fmt.Sprintf("Error: %v", err)
		}

		data := map[string]interface{}{
			"Text": text,
		}

		var buf strings.Builder
		if err := tmpl.Execute(&buf, data); err != nil {
			return fmt.Sprintf("Error: %v", err)
		}
		text = buf.String()
	}

	return text
}

// runBlocks executes configured blocks
func runBlocks(config Config) []Output {
	var outputs []Output

	for _, block := range config.Blocks {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		text := executeBlock(ctx, block)

		outputs = append(outputs, Output{
			Name:                block.Name,
			FullText:            text,
			Separator:           true,
			SeparatorBlockWidth: 9,
		})
	}

	return outputs
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: lebar <config>")
		os.Exit(1)
	}

	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Printf("Read error: %v\n", err)
		os.Exit(1)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		fmt.Printf("Parse error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(`{"version":1}`)
	fmt.Println("[")

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	first := true
	for range ticker.C {
		outputs := runBlocks(config)
		if !first {
			fmt.Print(",")
		}
		first = false
		jsonOutput, _ := json.Marshal(outputs)
		fmt.Printf("%s\n", jsonOutput)
	}
}
