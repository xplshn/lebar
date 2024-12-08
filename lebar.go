package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"text/template"
	"time"
	"unicode"

	"github.com/Masterminds/sprig/v3"
	"github.com/goccy/go-yaml"
	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
	"github.com/traefik/yaegi/stdlib/syscall"
	"github.com/traefik/yaegi/stdlib/unrestricted"
	"github.com/traefik/yaegi/stdlib/unsafe"
)

// SymbolList represents a named list of symbols
type SymbolList struct {
	Name    string   `yaml:"name"`
	Symbols []string `yaml:"symbols"`
}

// Config represents the tool's configuration
type Config struct {
	StopSignal  int          `yaml:"stop_signal"`
	ContSignal  int          `yaml:"cont_signal"`
	Separator   string       `yaml:"separator"`
	SymbolLists []SymbolList `yaml:"symbol_lists"`
	Blocks      []Block      `yaml:"blocks"`
	ClickEvents bool         `yaml:"-"`
}

// Block defines a status bar module
type Block struct {
	Name        string `yaml:"name"`
	Interval    int    `yaml:"interval"`
	Interpreter string `yaml:"interpreter"`
	Script      string `yaml:"script"`
	Format      string `json:"format"`
	OnClick     struct {
		Interpreter string `yaml:"interpreter"`
		Script      string `yaml:"script"`
	} `yaml:"on_click"`
}

// Output represents a bar item
type Output struct {
	Name                string      `json:"name"`
	FullText            string      `json:"full_text"`
	ShortText           string      `json:"short_text,omitempty"`
	Color               string      `json:"color,omitempty"`
	Background          string      `json:"background,omitempty"`
	Border              string      `json:"border,omitempty"`
	BorderTop           int         `json:"border_top,omitempty"`
	BorderRight         int         `json:"border_right,omitempty"`
	BorderBottom        int         `json:"border_bottom,omitempty"`
	BorderLeft          int         `json:"border_left,omitempty"`
	MinWidth            interface{} `json:"min_width,omitempty"`
	Align               string      `json:"align,omitempty"`
	Urgent              bool        `json:"urgent,omitempty"`
	Separator           bool        `json:"separator,omitempty"`
	SeparatorBlockWidth int         `json:"separator_block_width,omitempty"`
	Markup              string      `json:"markup,omitempty"`
}

var (
	defaultSymbols        = []string{"🟦", "🟩", "🟨", "🟫", "🟥"}
	defaultOver100Symbols = []string{"⚠️", "💥", "🆘"}
	debugLog              *log.Logger
)

// findSymbolList finds a symbol list by name in the configuration
func findSymbolList(config Config, name string) []string {
	for _, list := range config.SymbolLists {
		if list.Name == name {
			return list.Symbols
		}
	}
	return nil
}

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

// executeScript runs a script using the specified interpreter
func executeScript(ctx context.Context, interpreter, script string) (string, error) {
	if interpreter == "" {
		return "", fmt.Errorf("Interpreter not specified")
	}

	parts := strings.Fields(interpreter)
	if len(parts) == 0 {
		return "", fmt.Errorf("Interpreter format is invalid")
	}

	interpreterPath, err := exec.LookPath(parts[0])
	if err != nil || interpreterPath == "" {
		return "", fmt.Errorf("Interpreter '%s' does not exist", interpreter)
	}

	if interpreter == "yaegi" {
		return executeYaegi(script)
	}

	cmd := exec.CommandContext(ctx, parts[0], append(parts[1:], script)...)
	cmdOutput, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(cmdOutput)), nil
}

// executeBlock runs a block's script using the specified interpreter
func executeBlock(ctx context.Context, block Block, config Config) (string, error) {
	debugLog.Println("Executing block:", block.Name)

	output, err := executeScript(ctx, block.Interpreter, block.Script)
	if err != nil {
		return "", err
	}

	text := strings.TrimSpace(output)
	if block.Format != "" {
		tmpl, err := template.New("format").Funcs(template.FuncMap{
			"Symbol": func(args ...interface{}) string {
				if len(args) < 1 {
					return ""
				}

				var number float64
				strVal := fmt.Sprintf("%v", args[0])
				strVal = strings.TrimSpace(strings.TrimSuffix(strVal, "%"))
				num, err := strconv.ParseFloat(strVal, 64)
				if err != nil {
					return "?"
				}
				number = num

				symbolList := defaultSymbols
				over100SymbolList := defaultOver100Symbols

				if len(args) > 1 {
					switch arg := args[1].(type) {
					case string:
						customSymbolList := findSymbolList(config, arg)
						if customSymbolList != nil {
							symbolList = customSymbolList
						}
					case []string:
						if len(arg) > 0 {
							symbolList = arg
						}
					}
				}
				if len(args) > 2 {
					switch arg := args[2].(type) {
					case string:
						customOver100SymbolList := findSymbolList(config, arg)
						if customOver100SymbolList != nil {
							over100SymbolList = customOver100SymbolList
						}
					case []string:
						if len(arg) > 0 {
							over100SymbolList = arg
						}
					}
				}

				if len(symbolList) == 0 {
					symbolList = defaultSymbols
				}
				if len(over100SymbolList) == 0 {
					over100SymbolList = defaultOver100Symbols
				}

				if number <= 100 {
					numSymbols := len(symbolList)
					index := int((number / 100) * float64(numSymbols-1))
					return symbolList[index]
				} else {
					numSymbols := len(over100SymbolList)
					index := int(math.Min(float64(numSymbols-1), (number/100)*float64(numSymbols-1)))
					return over100SymbolList[index]
				}
			},
		}).Funcs(sprig.TxtFuncMap()).Parse(block.Format)
		if err != nil {
			return "", err
		}

		data := map[string]interface{}{
			"Text": text,
		}

		var buf strings.Builder
		if err := tmpl.Execute(&buf, data); err != nil {
			return "", err
		}
		text = buf.String()
	}

	return text, nil
}

// runBlocks executes configured blocks
func runBlocks(config Config) ([]Output, error) {
	var outputs []Output

	for _, block := range config.Blocks {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		text, err := executeBlock(ctx, block, config)
		if err != nil {
			return nil, err
		}

		outputs = append(outputs, Output{
			Name:                block.Name,
			FullText:            text,
			Separator:           true,
			SeparatorBlockWidth: 9,
		})
	}

	return outputs, nil
}

// NewEventFromRaw parses a raw JSON click event
func NewEventFromRaw(raw []byte) (*I3barClickEvent, error) {
	raw = bytes.TrimLeftFunc(raw, func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	})

	raw = bytes.TrimLeftFunc(raw, func(r rune) bool {
		return r != '{'
	})
	raw = bytes.TrimRightFunc(raw, func(r rune) bool {
		return r != '}'
	})

	debugLog.Printf("Processed raw input: %s", string(raw))

	ev := new(I3barClickEvent)
	if err := json.Unmarshal(raw, ev); err != nil {
		debugLog.Printf("JSON Unmarshal error: %v", err)
		debugLog.Printf("Problematic JSON: %s", string(raw))
		return nil, fmt.Errorf("failed to parse click event: %v", err)
	}
	return ev, nil
}

// handleClickEvents reads and processes click events from stdin with extensive logging
func handleClickEvents(config Config) {
	debugLog.Println("Starting handleClickEvents")
	defer debugLog.Println("Finished handleClickEvents")

	scanner := bufio.NewScanner(os.Stdin)

	if scanner.Scan() {
		debugLog.Printf("Initial line: %s\n", scanner.Text())
	}

	for scanner.Scan() {
		raw := scanner.Bytes()
		debugLog.Printf("Raw input line: %s\n", string(raw))

		if len(bytes.TrimSpace(raw)) == 0 {
			debugLog.Println("Skipping empty line")
			continue
		}
		if bytes.Equal(raw, []byte(",")) {
			debugLog.Println("Skipping comma")
			continue
		}

		ev, err := NewEventFromRaw(raw)
		if err != nil {
			debugLog.Printf("Error parsing click event: %v\n", err)
			continue
		}

		block := findBlockByName(config, ev.Name)
		if block == nil {
			debugLog.Printf("No block found for name: %s\n", ev.Name)
			continue
		}

		debugLog.Printf("Matched block: %+v\n", *block)
		debugLog.Printf("OnClick script: %s\n", block.OnClick.Script)

		if block.OnClick.Script == "" {
			debugLog.Printf("No onClick script for block: %s\n", block.Name)
			continue
		}

		interpreter := block.OnClick.Interpreter
		if interpreter == "" {
			interpreter = block.Interpreter
		}
		debugLog.Printf("Using interpreter: %s\n", interpreter)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		oldEnv := os.Environ()
		os.Setenv("BUTTON", ev.Button.String())
		os.Setenv("X", strconv.Itoa(ev.X))
		os.Setenv("Y", strconv.Itoa(ev.Y))
		defer func() {
			os.Clearenv()
			for _, env := range oldEnv {
				key, value, _ := strings.Cut(env, "=")
				os.Setenv(key, value)
			}
		}()

		debugLog.Printf("Executing OnClick script for block: %s\n", block.Name)
		debugLog.Printf("OnClick script details: %+v\n", block.OnClick)

		output, err := executeScript(ctx, interpreter, block.OnClick.Script)
		if err != nil {
			debugLog.Printf("Error executing OnClick script for block %s: %v\n", block.Name, err)
			fmt.Fprintf(os.Stderr, "Error executing OnClick script: %v\n", err)
		} else {
			debugLog.Printf("OnClick script output for block %s: %s\n", block.Name, output)
		}
	}

	if err := scanner.Err(); err != nil {
		debugLog.Printf("Error reading stdin: %v\n", err)
	}
}

// Helper method to convert eventButton to string
func (b eventButton) String() string {
	switch b {
	case ButtonLeft:
		return "Left"
	case ButtonMiddle:
		return "Middle"
	case ButtonRight:
		return "Right"
	case ButtonScrollUp:
		return "ScrollUp"
	case ButtonScrollDown:
		return "ScrollDown"
	default:
		return ""
	}
}

// findBlockByName finds a block by name in the configuration
func findBlockByName(config Config, name string) *Block {
	for i, block := range config.Blocks {
		if block.Name == name {
			return &config.Blocks[i]
		}
	}
	return nil
}

// I3barClickEvent represents a click event
type I3barClickEvent struct {
	Name       string      `json:"name"`
	Instance   string      `json:"instance"`
	X          int         `json:"x"`
	Y          int         `json:"y"`
	Button     eventButton `json:"button"`
	Event      int         `json:"event"`
	RelativeX  int         `json:"relative_x"`
	RelativeY  int         `json:"relative_y"`
	Width      int         `json:"width"`
	Height     int         `json:"height"`
	Scale      float64     `json:"scale,omitempty"`
}

// eventButton represents a button event
type eventButton int

const (
	ButtonLeft       eventButton = 1
	ButtonMiddle     eventButton = 2
	ButtonRight      eventButton = 3
	ButtonScrollUp   eventButton = 4
	ButtonScrollDown eventButton = 5
)

func main() {
	logFile, err := os.OpenFile("/tmp/lebar.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Failed to open debug file: %v", err)
	}
	defer logFile.Close()

	logger := log.New(logFile, "INFO: ", log.LstdFlags|log.Lmicroseconds)
	debugLog = log.New(logFile, "DEBUG: ", log.LstdFlags|log.Lmicroseconds)

	if len(os.Args) < 2 {
		logger.Println("Usage: lebar <config>")
		os.Exit(1)
	}

	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		logger.Printf("Read error: %v\n", err)
		os.Exit(1)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		logger.Printf("Parse error: %v\n", err)
		os.Exit(1)
	}

	if config.Separator == "" {
		config.Separator = "|"
	}

	config.ClickEvents = false
	for _, block := range config.Blocks {
		if block.OnClick.Script != "" {
			config.ClickEvents = true
			break
		}
	}

	header := map[string]interface{}{
		"version":      1,
		"stop_signal":  config.StopSignal,
		"cont_signal":  config.ContSignal,
		"click_events": config.ClickEvents,
	}

	headerJSON, err := json.Marshal(header)
	if err != nil {
		logger.Printf("Header JSON error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("%s\n", headerJSON)
	fmt.Println("[")

	if config.ClickEvents {
		go func() {
			handleClickEvents(config)
			fmt.Printf("\nTriggered\n")
			os.Exit(0)
		}()
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	first := true
	for range ticker.C {
		outputs, err := runBlocks(config)
		if err != nil {
			logger.Printf("Error: %v\n", err)
			continue
		}
		if !first {
			fmt.Print(",")
		}
		first = false
		jsonOutput, _ := json.Marshal(outputs)
		fmt.Printf("%s", jsonOutput)
	}
	fmt.Println("]")

	select {}
}
