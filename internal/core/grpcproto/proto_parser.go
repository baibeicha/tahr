package grpcproto

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

// TokenType represents lexical token category in protobuf syntax.
type TokenType int

const (
	TokenEOF TokenType = iota
	TokenIdent
	TokenString
	TokenNumber
	TokenSymbol
)

// Token represents a single lexical token.
type Token struct {
	Type  TokenType
	Value string
	Line  int
	Col   int
}

// Field represents a single field inside a Protobuf message.
type Field struct {
	Name         string            `json:"name"`
	Type         string            `json:"type"`
	Number       int               `json:"number"`
	IsRepeated   bool              `json:"is_repeated"`
	IsOptional   bool              `json:"is_optional"`
	IsRequired   bool              `json:"is_required"`
	IsMap        bool              `json:"is_map"`
	MapKeyType   string            `json:"map_key_type,omitempty"`
	MapValueType string            `json:"map_value_type,omitempty"`
	OneOfName    string            `json:"one_of_name,omitempty"`
	DefaultValue string            `json:"default_value,omitempty"`
	Options      map[string]string `json:"options,omitempty"`
	Comments     string            `json:"comments,omitempty"`
}

// OneOf represents a oneof union block inside a message.
type OneOf struct {
	Name   string  `json:"name"`
	Fields []Field `json:"fields"`
}

// EnumValue represents a single named integer value in an Enum.
type EnumValue struct {
	Name    string `json:"name"`
	Number  int    `json:"number"`
	Comment string `json:"comment,omitempty"`
}

// Enum represents a Protobuf enum definition.
type Enum struct {
	Name     string      `json:"name"`
	FullName string      `json:"full_name"`
	Values   []EnumValue `json:"values"`
	Comments string      `json:"comments,omitempty"`
}

// Message represents a Protobuf message definition.
type Message struct {
	Name           string            `json:"name"`
	FullName       string            `json:"full_name"`
	Fields         []Field           `json:"fields"`
	OneOfs         []OneOf           `json:"one_ofs,omitempty"`
	NestedMessages []Message         `json:"nested_messages,omitempty"`
	NestedEnums    []Enum            `json:"nested_enums,omitempty"`
	Options        map[string]string `json:"options,omitempty"`
	Comments       string            `json:"comments,omitempty"`
}

// RPCMethod represents an RPC procedure in a gRPC service.
type RPCMethod struct {
	Name            string            `json:"name"`
	RequestType     string            `json:"request_type"`
	ResponseType    string            `json:"response_type"`
	ClientStreaming bool              `json:"client_streaming"`
	ServerStreaming bool              `json:"server_streaming"`
	Options         map[string]string `json:"options,omitempty"`
	Comments        string            `json:"comments,omitempty"`
}

// Service represents a gRPC service definition.
type Service struct {
	Name     string            `json:"name"`
	FullName string            `json:"full_name"`
	Methods  []RPCMethod       `json:"methods"`
	Options  map[string]string `json:"options,omitempty"`
	Comments string            `json:"comments,omitempty"`
}

// ProtoFile represents a fully parsed .proto specification file.
type ProtoFile struct {
	FilePath string            `json:"file_path"`
	Syntax   string            `json:"syntax"` // "proto3" or "proto2"
	Package  string            `json:"package"`
	Imports  []string          `json:"imports"`
	Options  map[string]string `json:"options"`
	Services []Service         `json:"services"`
	Messages []Message         `json:"messages"`
	Enums    []Enum            `json:"enums"`
}

// ProtoWorkspace manages multiple parsed proto files and cross-references.
type ProtoWorkspace struct {
	RootPath string
	Files    []*ProtoFile
	messages map[string]*Message
	enums    map[string]*Enum
	services map[string]*Service
}

// NewProtoWorkspace initializes an empty workspace container.
func NewProtoWorkspace(rootPath string) *ProtoWorkspace {
	return &ProtoWorkspace{
		RootPath: rootPath,
		Files:    make([]*ProtoFile, 0),
		messages: make(map[string]*Message),
		enums:    make(map[string]*Enum),
		services: make(map[string]*Service),
	}
}

// AddFile registers a parsed ProtoFile and indexes its components.
func (ws *ProtoWorkspace) AddFile(pf *ProtoFile) {
	ws.Files = append(ws.Files, pf)

	var indexMsg func(msg *Message, prefix string)
	indexMsg = func(msg *Message, prefix string) {
		full := msg.Name
		if prefix != "" {
			full = prefix + "." + msg.Name
		}
		msg.FullName = full
		ws.messages[full] = msg
		ws.messages[msg.Name] = msg

		for i := range msg.NestedMessages {
			indexMsg(&msg.NestedMessages[i], full)
		}
		for i := range msg.NestedEnums {
			e := &msg.NestedEnums[i]
			eFull := full + "." + e.Name
			e.FullName = eFull
			ws.enums[eFull] = e
			ws.enums[e.Name] = e
		}
	}

	pkgPrefix := pf.Package
	for i := range pf.Messages {
		indexMsg(&pf.Messages[i], pkgPrefix)
	}

	for i := range pf.Enums {
		e := &pf.Enums[i]
		full := e.Name
		if pkgPrefix != "" {
			full = pkgPrefix + "." + e.Name
		}
		e.FullName = full
		ws.enums[full] = e
		ws.enums[e.Name] = e
	}

	for i := range pf.Services {
		s := &pf.Services[i]
		full := s.Name
		if pkgPrefix != "" {
			full = pkgPrefix + "." + s.Name
		}
		s.FullName = full
		ws.services[full] = s
		ws.services[s.Name] = s
	}
}

// FindMessage locates a message by qualified or simple name.
func (ws *ProtoWorkspace) FindMessage(name string) *Message {
	trimmed := strings.TrimPrefix(name, ".")
	if msg, ok := ws.messages[trimmed]; ok {
		return msg
	}
	// Fallback to suffix search (e.g. "UserRequest" matching "auth.v1.UserRequest")
	for k, v := range ws.messages {
		if strings.HasSuffix(k, "."+trimmed) || k == trimmed {
			return v
		}
	}
	return nil
}

// FindEnum locates an enum by qualified or simple name.
func (ws *ProtoWorkspace) FindEnum(name string) *Enum {
	trimmed := strings.TrimPrefix(name, ".")
	if en, ok := ws.enums[trimmed]; ok {
		return en
	}
	for k, v := range ws.enums {
		if strings.HasSuffix(k, "."+trimmed) || k == trimmed {
			return v
		}
	}
	return nil
}

// FindService locates a service by qualified or simple name.
func (ws *ProtoWorkspace) FindService(name string) *Service {
	trimmed := strings.TrimPrefix(name, ".")
	if svc, ok := ws.services[trimmed]; ok {
		return svc
	}
	for k, v := range ws.services {
		if strings.HasSuffix(k, "."+trimmed) || k == trimmed {
			return v
		}
	}
	return nil
}

// AllServices returns all registered services across the workspace.
func (ws *ProtoWorkspace) AllServices() []Service {
	res := make([]Service, 0)
	for _, f := range ws.Files {
		res = append(res, f.Services...)
	}
	return res
}

// AllMessages returns all top-level messages across the workspace.
func (ws *ProtoWorkspace) AllMessages() []Message {
	res := make([]Message, 0)
	for _, f := range ws.Files {
		res = append(res, f.Messages...)
	}
	return res
}

// DiscoverProtoFiles scans a directory recursively for .proto files.
// It ignores common build, vendor, and hidden directories.
func DiscoverProtoFiles(workspaceRoot string) ([]string, error) {
	var protoFiles []string
	err := filepath.WalkDir(workspaceRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path != workspaceRoot && (strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(filepath.Ext(name), ".proto") {
			protoFiles = append(protoFiles, path)
		}
		return nil
	})
	return protoFiles, err
}

// LoadWorkspace scans the directory, parses all proto files, and returns a unified ProtoWorkspace.
func LoadWorkspace(workspaceRoot string) (*ProtoWorkspace, error) {
	ws := NewProtoWorkspace(workspaceRoot)
	files, err := DiscoverProtoFiles(workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("failed to discover proto files: %w", err)
	}

	for _, file := range files {
		pf, err := ParseProtoFile(file)
		if err != nil {
			// Skip files that fail to parse gracefully or continue loading others
			continue
		}
		ws.AddFile(pf)
	}
	return ws, nil
}

// ParseProtoFile reads and parses a single .proto file from disk.
func ParseProtoFile(filePath string) (*ProtoFile, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read proto file %s: %w", filePath, err)
	}
	return ParseProtoString(string(data), filePath)
}

// ParseProtoString parses .proto content from a string buffer.
func ParseProtoString(content, filePath string) (*ProtoFile, error) {
	tokens, err := tokenize(content)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filePath, err)
	}

	p := &protoParser{
		tokens:   tokens,
		pos:      0,
		filePath: filePath,
		file: &ProtoFile{
			FilePath: filePath,
			Syntax:   "proto3", // default
			Options:  make(map[string]string),
			Services: make([]Service, 0),
			Messages: make([]Message, 0),
			Enums:    make([]Enum, 0),
			Imports:  make([]string, 0),
		},
	}

	if err := p.parseFile(); err != nil {
		return nil, fmt.Errorf("%s: %w", filePath, err)
	}

	return p.file, nil
}

// --- Lexer / Tokenizer ---

func tokenize(src string) ([]Token, error) {
	var tokens []Token
	reader := bufio.NewReader(strings.NewReader(src))
	line := 1
	col := 0

	readRune := func() (rune, error) {
		r, _, err := reader.ReadRune()
		if err == nil {
			if r == '\n' {
				line++
				col = 0
			} else {
				col++
			}
		}
		return r, err
	}

	unreadRune := func(r rune) {
		_ = reader.UnreadRune()
		if r == '\n' {
			line--
			col = 0
		} else {
			col--
		}
	}

	for {
		r, err := readRune()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		// Whitespace
		if unicode.IsSpace(r) {
			continue
		}

		// Comments
		if r == '/' {
			next, err := readRune()
			if err == nil && next == '/' {
				// Line comment
				for {
					c, err := readRune()
					if err != nil || c == '\n' {
						break
					}
				}
				continue
			} else if err == nil && next == '*' {
				// Block comment
				for {
					c, err := readRune()
					if err != nil {
						break
					}
					if c == '*' {
						c2, err := readRune()
						if err == nil && c2 == '/' {
							break
						}
						if err == nil {
							unreadRune(c2)
						}
					}
				}
				continue
			} else if err == nil {
				unreadRune(next)
			}
		}

		// String literal
		if r == '"' || r == '\'' {
			quote := r
			var sb strings.Builder
			for {
				c, err := readRune()
				if err != nil {
					return nil, fmt.Errorf("unterminated string at line %d", line)
				}
				if c == quote {
					break
				}
				if c == '\\' {
					esc, err := readRune()
					if err != nil {
						return nil, fmt.Errorf("escape error at line %d", line)
					}
					switch esc {
					case 'n':
						sb.WriteRune('\n')
					case 'r':
						sb.WriteRune('\r')
					case 't':
						sb.WriteRune('\t')
					case '\\':
						sb.WriteRune('\\')
					case '"':
						sb.WriteRune('"')
					case '\'':
						sb.WriteRune('\'')
					default:
						sb.WriteRune(esc)
					}
				} else {
					sb.WriteRune(c)
				}
			}
			tokens = append(tokens, Token{Type: TokenString, Value: sb.String(), Line: line, Col: col})
			continue
		}

		// Symbols
		if strings.ContainsRune("{}(<>)[]=;,:", r) {
			tokens = append(tokens, Token{Type: TokenSymbol, Value: string(r), Line: line, Col: col})
			continue
		}

		// Identifiers or numbers
		if unicode.IsLetter(r) || r == '_' || r == '.' {
			var sb strings.Builder
			sb.WriteRune(r)
			for {
				c, err := readRune()
				if err != nil {
					break
				}
				if unicode.IsLetter(c) || unicode.IsDigit(c) || c == '_' || c == '.' {
					sb.WriteRune(c)
				} else {
					unreadRune(c)
					break
				}
			}
			val := sb.String()
			tokens = append(tokens, Token{Type: TokenIdent, Value: val, Line: line, Col: col})
			continue
		}

		if unicode.IsDigit(r) || r == '-' || r == '+' {
			var sb strings.Builder
			sb.WriteRune(r)
			for {
				c, err := readRune()
				if err != nil {
					break
				}
				if unicode.IsDigit(c) || c == '.' || c == 'x' || c == 'X' || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
					sb.WriteRune(c)
				} else {
					unreadRune(c)
					break
				}
			}
			tokens = append(tokens, Token{Type: TokenNumber, Value: sb.String(), Line: line, Col: col})
			continue
		}
	}

	tokens = append(tokens, Token{Type: TokenEOF, Line: line, Col: col})
	return tokens, nil
}

// --- Parser ---

type protoParser struct {
	tokens   []Token
	pos      int
	filePath string
	file     *ProtoFile
}

func (p *protoParser) peek() Token {
	if p.pos >= len(p.tokens) {
		return Token{Type: TokenEOF}
	}
	return p.tokens[p.pos]
}

func (p *protoParser) next() Token {
	tok := p.peek()
	if p.pos < len(p.tokens) {
		p.pos++
	}
	return tok
}

func (p *protoParser) matchSymbol(sym string) bool {
	if p.peek().Type == TokenSymbol && p.peek().Value == sym {
		p.next()
		return true
	}
	return false
}

func (p *protoParser) expectSymbol(sym string) error {
	tok := p.next()
	if tok.Type != TokenSymbol || tok.Value != sym {
		return fmt.Errorf("expected '%s', got '%s' at line %d", sym, tok.Value, tok.Line)
	}
	return nil
}

func (p *protoParser) skipUntilSemicolon() {
	braceDepth := 0
	for {
		tok := p.peek()
		if tok.Type == TokenEOF {
			return
		}
		if tok.Type == TokenSymbol {
			if tok.Value == "{" {
				braceDepth++
			} else if tok.Value == "}" {
				if braceDepth > 0 {
					braceDepth--
				}
			} else if tok.Value == ";" && braceDepth == 0 {
				p.next()
				return
			}
		}
		p.next()
	}
}

func (p *protoParser) parseFile() error {
	for {
		tok := p.peek()
		if tok.Type == TokenEOF {
			break
		}

		if tok.Type == TokenIdent {
			switch tok.Value {
			case "syntax":
				p.next()
				if err := p.expectSymbol("="); err != nil {
					return err
				}
				valTok := p.next()
				if valTok.Type != TokenString {
					return fmt.Errorf("expected string for syntax version at line %d", valTok.Line)
				}
				p.file.Syntax = valTok.Value
				_ = p.matchSymbol(";")

			case "package":
				p.next()
				pkgName := p.parseQualifiedIdentifier()
				p.file.Package = pkgName
				_ = p.matchSymbol(";")

			case "import":
				p.next()
				// Handle optional "public" or "weak"
				if p.peek().Type == TokenIdent && (p.peek().Value == "public" || p.peek().Value == "weak") {
					p.next()
				}
				impTok := p.next()
				if impTok.Type == TokenString {
					p.file.Imports = append(p.file.Imports, impTok.Value)
				}
				_ = p.matchSymbol(";")

			case "option":
				p.next()
				key, val := p.parseOption()
				if key != "" {
					p.file.Options[key] = val
				}

			case "service":
				p.next()
				svc, err := p.parseService()
				if err != nil {
					return err
				}
				p.file.Services = append(p.file.Services, svc)

			case "message":
				p.next()
				msg, err := p.parseMessage()
				if err != nil {
					return err
				}
				p.file.Messages = append(p.file.Messages, msg)

			case "enum":
				p.next()
				en, err := p.parseEnum()
				if err != nil {
					return err
				}
				p.file.Enums = append(p.file.Enums, en)

			default:
				// Skip unrecognized top-level elements gracefully
				p.skipUntilSemicolon()
			}
		} else {
			p.next()
		}
	}
	return nil
}

func (p *protoParser) parseQualifiedIdentifier() string {
	var parts []string
	for {
		tok := p.peek()
		if tok.Type == TokenIdent || tok.Type == TokenNumber {
			parts = append(parts, tok.Value)
			p.next()
		} else {
			break
		}
	}
	return strings.Join(parts, "")
}

func (p *protoParser) parseOption() (string, string) {
	var keyBuilder strings.Builder
	for {
		tok := p.peek()
		if tok.Type == TokenSymbol && tok.Value == "=" {
			p.next()
			break
		}
		if tok.Type == TokenSymbol && tok.Value == ";" {
			return keyBuilder.String(), ""
		}
		if tok.Type == TokenEOF {
			return keyBuilder.String(), ""
		}
		keyBuilder.WriteString(tok.Value)
		p.next()
	}

	// Parse option value
	var valBuilder strings.Builder
	braceDepth := 0
	for {
		tok := p.peek()
		if tok.Type == TokenEOF {
			break
		}
		if tok.Type == TokenSymbol {
			if tok.Value == "{" {
				braceDepth++
			} else if tok.Value == "}" {
				braceDepth--
			} else if tok.Value == ";" && braceDepth == 0 {
				p.next()
				break
			}
		}
		valBuilder.WriteString(tok.Value)
		p.next()
	}
	return strings.TrimSpace(keyBuilder.String()), strings.TrimSpace(valBuilder.String())
}

func (p *protoParser) parseService() (Service, error) {
	nameTok := p.next()
	svc := Service{
		Name:    nameTok.Value,
		Methods: make([]RPCMethod, 0),
		Options: make(map[string]string),
	}

	if err := p.expectSymbol("{"); err != nil {
		return svc, err
	}

	for {
		tok := p.peek()
		if tok.Type == TokenEOF || (tok.Type == TokenSymbol && tok.Value == "}") {
			p.next()
			break
		}

		if tok.Type == TokenIdent {
			switch tok.Value {
			case "rpc":
				p.next()
				rpc, err := p.parseRPC()
				if err != nil {
					return svc, err
				}
				svc.Methods = append(svc.Methods, rpc)
			case "option":
				p.next()
				k, v := p.parseOption()
				if k != "" {
					svc.Options[k] = v
				}
			default:
				p.skipUntilSemicolon()
			}
		} else {
			p.next()
		}
	}

	return svc, nil
}

func (p *protoParser) parseRPC() (RPCMethod, error) {
	nameTok := p.next()
	rpc := RPCMethod{
		Name:    nameTok.Value,
		Options: make(map[string]string),
	}

	// ( [stream] ReqType )
	if err := p.expectSymbol("("); err != nil {
		return rpc, err
	}
	if p.peek().Type == TokenIdent && p.peek().Value == "stream" {
		rpc.ClientStreaming = true
		p.next()
	}
	rpc.RequestType = p.parseQualifiedIdentifier()
	if err := p.expectSymbol(")"); err != nil {
		return rpc, err
	}

	// returns ( [stream] RespType )
	retTok := p.next()
	if retTok.Value != "returns" {
		return rpc, fmt.Errorf("expected 'returns', got '%s' at line %d", retTok.Value, retTok.Line)
	}

	if err := p.expectSymbol("("); err != nil {
		return rpc, err
	}
	if p.peek().Type == TokenIdent && p.peek().Value == "stream" {
		rpc.ServerStreaming = true
		p.next()
	}
	rpc.ResponseType = p.parseQualifiedIdentifier()
	if err := p.expectSymbol(")"); err != nil {
		return rpc, err
	}

	// Body or semicolon
	if p.matchSymbol(";") {
		return rpc, nil
	}

	if p.matchSymbol("{") {
		for {
			tok := p.peek()
			if tok.Type == TokenEOF || (tok.Type == TokenSymbol && tok.Value == "}") {
				p.next()
				break
			}
			if tok.Type == TokenIdent && tok.Value == "option" {
				p.next()
				k, v := p.parseOption()
				if k != "" {
					rpc.Options[k] = v
				}
			} else {
				p.skipUntilSemicolon()
			}
		}
	}

	return rpc, nil
}

func (p *protoParser) parseMessage() (Message, error) {
	nameTok := p.next()
	msg := Message{
		Name:           nameTok.Value,
		Fields:         make([]Field, 0),
		OneOfs:         make([]OneOf, 0),
		NestedMessages: make([]Message, 0),
		NestedEnums:    make([]Enum, 0),
		Options:        make(map[string]string),
	}

	if err := p.expectSymbol("{"); err != nil {
		return msg, err
	}

	for {
		tok := p.peek()
		if tok.Type == TokenEOF || (tok.Type == TokenSymbol && tok.Value == "}") {
			p.next()
			break
		}

		if tok.Type == TokenIdent {
			switch tok.Value {
			case "message":
				p.next()
				nested, err := p.parseMessage()
				if err != nil {
					return msg, err
				}
				msg.NestedMessages = append(msg.NestedMessages, nested)

			case "enum":
				p.next()
				nestedEnum, err := p.parseEnum()
				if err != nil {
					return msg, err
				}
				msg.NestedEnums = append(msg.NestedEnums, nestedEnum)

			case "oneof":
				p.next()
				oneof, err := p.parseOneOf()
				if err != nil {
					return msg, err
				}
				msg.OneOfs = append(msg.OneOfs, oneof)
				for _, f := range oneof.Fields {
					msg.Fields = append(msg.Fields, f)
				}

			case "option":
				p.next()
				k, v := p.parseOption()
				if k != "" {
					msg.Options[k] = v
				}

			case "reserved", "extensions":
				p.skipUntilSemicolon()

			default:
				// Message field
				f, err := p.parseField()
				if err != nil {
					p.skipUntilSemicolon()
					continue
				}
				msg.Fields = append(msg.Fields, f)
			}
		} else {
			p.next()
		}
	}

	return msg, nil
}

func (p *protoParser) parseField() (Field, error) {
	f := Field{
		Options: make(map[string]string),
	}

	first := p.next()

	// Repeated / Optional / Required prefixes
	if first.Value == "repeated" {
		f.IsRepeated = true
		first = p.next()
	} else if first.Value == "optional" {
		f.IsOptional = true
		first = p.next()
	} else if first.Value == "required" {
		f.IsRequired = true
		first = p.next()
	}

	// Check for map<Key, Value>
	if first.Value == "map" && p.peek().Type == TokenSymbol && p.peek().Value == "<" {
		p.next() // consume '<'
		keyType := p.parseQualifiedIdentifier()
		if err := p.expectSymbol(","); err != nil {
			return f, err
		}
		valType := p.parseQualifiedIdentifier()
		if err := p.expectSymbol(">"); err != nil {
			return f, err
		}
		f.IsMap = true
		f.Type = fmt.Sprintf("map<%s, %s>", keyType, valType)
		f.MapKeyType = keyType
		f.MapValueType = valType
	} else {
		// Normal field type (may be qualified e.g. foo.bar.Baz)
		var typeParts []string
		typeParts = append(typeParts, first.Value)
		for {
			tok := p.peek()
			if tok.Type == TokenIdent && (strings.HasPrefix(tok.Value, ".") || strings.HasSuffix(typeParts[len(typeParts)-1], ".")) {
				typeParts = append(typeParts, tok.Value)
				p.next()
			} else {
				break
			}
		}
		f.Type = strings.Join(typeParts, "")
	}

	// Field name
	nameTok := p.next()
	f.Name = nameTok.Value

	// '='
	if err := p.expectSymbol("="); err != nil {
		return f, err
	}

	// Field number
	numTok := p.next()
	num, _ := strconv.Atoi(numTok.Value)
	f.Number = num

	// Optional field options: [json_name = "foo", (ext) = 10]
	if p.matchSymbol("[") {
		for {
			tok := p.peek()
			if tok.Type == TokenEOF || (tok.Type == TokenSymbol && tok.Value == "]") {
				p.next()
				break
			}
			optKey := p.parseOptionKey()
			optVal := ""
			if p.matchSymbol("=") {
				optVal = p.parseOptionValue()
			}
			if optKey != "" {
				f.Options[optKey] = optVal
				if optKey == "default" {
					f.DefaultValue = optVal
				}
			}
			_ = p.matchSymbol(",")
		}
	}

	_ = p.matchSymbol(";")
	return f, nil
}

func (p *protoParser) parseOptionKey() string {
	var sb strings.Builder
	for {
		tok := p.peek()
		if tok.Type == TokenSymbol && (tok.Value == "=" || tok.Value == "," || tok.Value == "]" || tok.Value == ";") {
			break
		}
		if tok.Type == TokenEOF {
			break
		}
		sb.WriteString(tok.Value)
		p.next()
	}
	return strings.TrimSpace(sb.String())
}

func (p *protoParser) parseOptionValue() string {
	var sb strings.Builder
	bracketDepth := 0
	for {
		tok := p.peek()
		if tok.Type == TokenEOF {
			break
		}
		if tok.Type == TokenSymbol {
			if tok.Value == "{" || tok.Value == "[" {
				bracketDepth++
			} else if tok.Value == "}" || tok.Value == "]" {
				if bracketDepth > 0 {
					bracketDepth--
				} else {
					break
				}
			} else if tok.Value == "," && bracketDepth == 0 {
				break
			} else if tok.Value == ";" && bracketDepth == 0 {
				break
			}
		}
		sb.WriteString(tok.Value)
		p.next()
	}
	return strings.TrimSpace(sb.String())
}

func (p *protoParser) parseOneOf() (OneOf, error) {
	nameTok := p.next()
	oneof := OneOf{
		Name:   nameTok.Value,
		Fields: make([]Field, 0),
	}

	if err := p.expectSymbol("{"); err != nil {
		return oneof, err
	}

	for {
		tok := p.peek()
		if tok.Type == TokenEOF || (tok.Type == TokenSymbol && tok.Value == "}") {
			p.next()
			break
		}

		if tok.Type == TokenIdent {
			f, err := p.parseField()
			if err != nil {
				p.skipUntilSemicolon()
				continue
			}
			f.OneOfName = oneof.Name
			oneof.Fields = append(oneof.Fields, f)
		} else {
			p.next()
		}
	}

	return oneof, nil
}

func (p *protoParser) parseEnum() (Enum, error) {
	nameTok := p.next()
	en := Enum{
		Name:   nameTok.Value,
		Values: make([]EnumValue, 0),
	}

	if err := p.expectSymbol("{"); err != nil {
		return en, err
	}

	for {
		tok := p.peek()
		if tok.Type == TokenEOF || (tok.Type == TokenSymbol && tok.Value == "}") {
			p.next()
			break
		}

		if tok.Type == TokenIdent {
			if tok.Value == "option" || tok.Value == "reserved" {
				p.skipUntilSemicolon()
				continue
			}

			valName := tok.Value
			p.next()
			if p.matchSymbol("=") {
				numTok := p.next()
				num, _ := strconv.Atoi(numTok.Value)
				// Skip optional [deprecated = true]
				if p.matchSymbol("[") {
					for {
						t := p.next()
						if t.Type == TokenEOF || (t.Type == TokenSymbol && t.Value == "]") {
							break
						}
					}
				}
				_ = p.matchSymbol(";")
				en.Values = append(en.Values, EnumValue{
					Name:   valName,
					Number: num,
				})
			} else {
				p.skipUntilSemicolon()
			}
		} else {
			p.next()
		}
	}

	return en, nil
}
