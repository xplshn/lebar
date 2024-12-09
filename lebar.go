package main

import (
	"bufio"
	"io"
	"bytes"
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode"
	"text/template"

	"github.com/Masterminds/sprig/v3" // TEMPLATE FUNCTIONS
	"github.com/goccy/go-json" // IO
	"github.com/goccy/go-yaml" // CFG
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

var (
	defaultSymbols        = []string{"🟦", "🟩", "🟨", "🟫", "🟥"}
	defaultOver100Symbols = []string{"⚠️", "💥", "🆘"}
	logger              *log.Logger
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

	cmd := exec.CommandContext(ctx, parts[0], append(parts[1:], script)...)
	cmdOutput, err := cmd.Output()
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(cmdOutput)), nil
}

// executeBlock runs a block's script using the specified interpreter
func executeBlock(ctx context.Context, block Block, config Config) (string, error) {
	logger.Println("Executing block:", block.Name)

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
		
		        // Parse the first (mandatory) argument
		        var number float64
		        strVal := fmt.Sprintf("%v", args[0])
		        strVal = strings.TrimSpace(strings.TrimSuffix(strVal, "%"))
		        num, err := strconv.ParseFloat(strVal, 64)
		        if err != nil {
		            return "?"
		        }
		        number = num
		
		        // Set default values
		        symbolList := defaultSymbols
		        over100SymbolList := defaultOver100Symbols
		        scale := 1.0 // Default scale is 1 (no scaling)
		
		        // Parse the second (optional) argument: custom symbol list
		        if len(args) > 1 {
		            switch arg := args[1].(type) {
		            case []string:
		                if len(arg) > 0 {
		                    symbolList = arg
		                }
		            case string:
		                customSymbolList := findSymbolList(config, arg)
		                if customSymbolList != nil {
		                    symbolList = customSymbolList
		                }
		            }
		        }
		
		        // Parse the third (optional) argument: custom over-100% symbol list or scale
		        if len(args) > 2 {
		            switch arg := args[2].(type) {
		            case []string:
		                if len(arg) > 0 {
		                    over100SymbolList = arg
		                }
		            case string:
		                customOver100SymbolList := findSymbolList(config, arg)
		                if customOver100SymbolList != nil {
		                    over100SymbolList = customOver100SymbolList
		                } else {
		                    // If it's not a symbol list name, treat it as scale
		                    if scaleArg, err := strconv.ParseFloat(arg, 64); err == nil && scaleArg > 0 {
		                        scale = scaleArg
		                    }
		                }
		            case float64:
		                if arg > 0 {
		                    scale = arg
		                }
		            }
		        }
		
		        // Parse the fourth (optional) argument: scale (if not already set)
		        if len(args) > 3 {
		            if scaleArg, ok := args[3].(float64); ok && scaleArg > 0 {
		                scale = scaleArg
		            }
		        }
		
		        // Validate and adjust lists if needed
		        if len(symbolList) == 0 {
		            symbolList = defaultSymbols
		        }
		        if len(over100SymbolList) == 0 {
		            over100SymbolList = defaultOver100Symbols
		        }
		
		        // Apply scale to the number
		        scaledNumber := number / scale
		
		        // Determine the appropriate symbol
		        if scaledNumber <= 100 {
		            numSymbols := len(symbolList)
		            index := int((scaledNumber / 100) * float64(numSymbols-1))
		            return symbolList[index]
		        } else {
		            numSymbols := len(over100SymbolList)
		            index := int(math.Min(float64(numSymbols-1), (scaledNumber/100)*float64(numSymbols-1)))
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

	logger.Printf("Processed raw input: %s", string(raw))

	ev := new(I3barClickEvent)
	if err := json.Unmarshal(raw, ev); err != nil {
		logger.Printf("JSON Unmarshal error: %v", err)
		logger.Printf("Problematic JSON: %s", string(raw))
		return nil, fmt.Errorf("failed to parse click event: %v", err)
	}
	return ev, nil
}

// handleClickEvents reads and processes click events from stdin with extensive logging
func handleClickEvents(config Config) {
	logger.Println("Starting handleClickEvents")
	defer logger.Println("Finished handleClickEvents")

	scanner := bufio.NewScanner(os.Stdin)

	if scanner.Scan() {
		logger.Printf("Initial line: %s\n", scanner.Text())
	}

	for scanner.Scan() {
		raw := scanner.Bytes()
		logger.Printf("Raw input line: %s\n", string(raw))

		if len(bytes.TrimSpace(raw)) == 0 {
			logger.Println("Skipping empty line")
			continue
		}
		if bytes.Equal(raw, []byte(",")) {
			logger.Println("Skipping comma")
			continue
		}

		ev, err := NewEventFromRaw(raw)
		if err != nil {
			logger.Printf("Error parsing click event: %v\n", err)
			continue
		}

		block := findBlockByName(config, ev.Name)
		if block == nil {
			logger.Printf("No block found for name: %s\n", ev.Name)
			continue
		}

		logger.Printf("Matched block: %+v\n", *block)
		logger.Printf("OnClick script: %s\n", block.OnClick.Script)

		if block.OnClick.Script == "" {
			logger.Printf("No onClick script for block: %s\n", block.Name)
			continue
		}

		interpreter := block.OnClick.Interpreter
		if interpreter == "" {
			interpreter = block.Interpreter
		}
		logger.Printf("Using interpreter: %s\n", interpreter)

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

		logger.Printf("Executing OnClick script for block: %s\n", block.Name)
		logger.Printf("OnClick script details: %+v\n", block.OnClick)

		output, err := executeScript(ctx, interpreter, block.OnClick.Script)
		if err != nil {
			logger.Printf("Error executing OnClick script for block %s: %v\n", block.Name, err)
			fmt.Fprintf(os.Stderr, "Error executing OnClick script: %v\n", err)
		} else {
			logger.Printf("OnClick script output for block %s: %s\n", block.Name, output)
		}
	}

	if err := scanner.Err(); err != nil {
		logger.Printf("Error reading stdin: %v\n", err)
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

// initLogger initializes the logger based on LEBAR_DEBUG environment variable
func initLogger(logFile *os.File) *log.Logger {
	debugMode, _ := strconv.ParseBool(os.Getenv("LEBAR_DEBUG"))
	
	// If debug mode is not set or false, discard logs
	if !debugMode {
		return log.New(io.Discard, "", 0)
	}

	// If debug mode is true, log to file with standard formatting
	return log.New(logFile, "DEBUG: ", log.LstdFlags|log.Lmicroseconds)
}

func main() {
	logFile, err := os.OpenFile("/tmp/lebar.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Failed to open debug file: %v", err)
	}
	defer logFile.Close()

	// Initialize logger using environment variable
	logger = initLogger(logFile)

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
