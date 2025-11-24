package harmonydb

import (
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var Logger *zap.Logger

// DebugConfig holds debug mode configuration
type DebugConfig struct {
	Enabled               bool
	Components            map[string]bool
	PerformanceMonitoring bool
}

var debugConfig *DebugConfig

// Debug mode detection from environment variables
func detectDebugMode() *DebugConfig {
	config := &DebugConfig{
		Components: make(map[string]bool),
	}

	// Check HARMONYDB_DEBUG
	if debug := os.Getenv("HARMONYDB_DEBUG"); debug != "" {
		config.Enabled = strings.ToLower(debug) == "true"
	}

	// Check HARMONYDB_DEBUG_COMPONENTS
	if components := os.Getenv("HARMONYDB_DEBUG_COMPONENTS"); components != "" {
		for _, comp := range strings.Split(components, ",") {
			config.Components[strings.TrimSpace(comp)] = true
		}
	}

	// Check HARMONYDB_DEBUG_PERF
	if perf := os.Getenv("HARMONYDB_DEBUG_PERF"); perf != "" {
		config.PerformanceMonitoring = strings.ToLower(perf) == "true"
	}

	return config
}

// Test context detection utilities
func detectTestContext() (bool, string) {
	// Check call stack for test functions
	for i := 1; i <= 10; i++ {
		pc, file, _, ok := runtime.Caller(i)
		if !ok {
			break
		}

		fn := runtime.FuncForPC(pc)
		if fn != nil {
			fnName := fn.Name()
			// Check if we're in a test function
			if strings.Contains(fnName, ".Test") || strings.Contains(fnName, ".Benchmark") {
				// Extract test file name for log file naming
				if strings.HasSuffix(file, "_test.go") {
					baseName := strings.TrimSuffix(file, "_test.go")
					parts := strings.Split(baseName, "/")
					if len(parts) > 0 {
						return true, parts[len(parts)-1] + "_test"
					}
				}
				return true, "test"
			}
		}

		// Check if file is a test file
		if strings.HasSuffix(file, "_test.go") {
			baseName := strings.TrimSuffix(file, "_test.go")
			parts := strings.Split(baseName, "/")
			if len(parts) > 0 {
				return true, parts[len(parts)-1] + "_test"
			}
		}
	}

	return false, ""
}

// Check if debug is enabled for a specific component
func isDebugEnabledForComponent(component string) bool {
	if debugConfig == nil {
		return false
	}

	// If global debug is enabled and no component filter, enable for all
	if debugConfig.Enabled && len(debugConfig.Components) == 0 {
		return true
	}

	// Check component-specific setting
	return debugConfig.Components[component]
}

// DebugFilterCore wraps zapcore.Core to provide component-based debug filtering
type DebugFilterCore struct {
	zapcore.Core
	debugConfig *DebugConfig
}

// NewDebugFilterCore creates a new DebugFilterCore wrapper
func NewDebugFilterCore(core zapcore.Core, config *DebugConfig) *DebugFilterCore {
	return &DebugFilterCore{
		Core:        core,
		debugConfig: config,
	}
}

// Check implements zapcore.Core interface with debug filtering logic
func (c *DebugFilterCore) Check(entry zapcore.Entry, checkedEntry *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	// For debug level entries, apply component-based filtering
	if entry.Level == zap.DebugLevel {
		if !c.isDebugEntryEnabled(checkedEntry) {
			return checkedEntry // Skip this debug entry
		}
	}

	return c.Core.Check(entry, checkedEntry)
}

// Write implements zapcore.Core interface
func (c *DebugFilterCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	// For debug level, check if component debug is enabled
	if entry.Level == zap.DebugLevel {
		component := c.extractComponentFromFields(fields)
		if component != "" && !isDebugEnabledForComponent(component) {
			return nil // Skip writing this debug entry
		}
	}

	return c.Core.Write(entry, fields)
}

// Sync implements zapcore.Core interface
func (c *DebugFilterCore) Sync() error {
	return c.Core.Sync()
}

// With implements zapcore.Core interface
func (c *DebugFilterCore) With(fields []zapcore.Field) zapcore.Core {
	return &DebugFilterCore{
		Core:        c.Core.With(fields),
		debugConfig: c.debugConfig,
	}
}

// Helper method to check if debug entry should be enabled
func (c *DebugFilterCore) isDebugEntryEnabled(checkedEntry *zapcore.CheckedEntry) bool {
	if c.debugConfig == nil {
		return false
	}

	// If global debug enabled and no component filters, allow all debug
	if c.debugConfig.Enabled && len(c.debugConfig.Components) == 0 {
		return true
	}

	// For component-specific filtering, we'll check in Write method where we have access to fields
	return c.debugConfig.Enabled
}

// Extract component name from zap fields
func (c *DebugFilterCore) extractComponentFromFields(fields []zapcore.Field) string {
	for _, field := range fields {
		if field.Key == "component" && field.Type == zapcore.StringType {
			return field.String
		}
	}
	return ""
}

func InitLogger(port int, test bool) error {
	// Detect debug mode from environment variables
	debugConfig = detectDebugMode()

	config := zap.NewProductionConfig()

	// Set log level based on debug mode
	logLevel := zap.InfoLevel
	if debugConfig.Enabled {
		logLevel = zap.DebugLevel
	}
	config.Level = zap.NewAtomicLevelAt(logLevel)

	// Determine log directory and file path
	logDir := "/var/log/harmonydb"
	if _, err := os.Stat("/opt/homebrew/var/log"); err == nil {
		logDir = "/opt/homebrew/var/log" // Local development
	}

	if test {
		logDir = "."
	}

	// Check for test context to potentially override log file name
	isTest, testName := detectTestContext()
	var logPath string

	if isTest && testName != "" {
		// Use test-specific log file
		logPath = fmt.Sprintf("%s/%s.log", logDir, testName)
	} else {
		// Use standard port-based log file
		logPath = fmt.Sprintf("%s/harmony_%d.log", logDir, port)
	}

	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return err
	}

	// Create encoder config for structured logging
	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "timestamp",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		FunctionKey:    zapcore.OmitKey,
		MessageKey:     "message",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     zapcore.ISO8601TimeEncoder,
		EncodeDuration: zapcore.SecondsDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	// Create console encoder for stdout
	consoleEncoder := zapcore.NewConsoleEncoder(encoderConfig)
	// Create JSON encoder for file
	fileEncoder := zapcore.NewJSONEncoder(encoderConfig)

	// Create multi-output core
	teeCore := zapcore.NewTee(
		zapcore.NewCore(consoleEncoder, zapcore.AddSync(os.Stdout), logLevel),
		zapcore.NewCore(fileEncoder, zapcore.AddSync(logFile), logLevel),
	)

	// Wrap with debug filter core for component-based filtering
	var core zapcore.Core
	if debugConfig.Enabled {
		core = NewDebugFilterCore(teeCore, debugConfig)
	} else {
		core = teeCore
	}

	Logger = zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))

	return nil
}

func GetLogger() *zap.Logger {
	if Logger == nil {
		// For tests, use a no-op logger
		Logger = zap.NewNop()
	}
	return Logger
}

func Sync() {
	if Logger != nil {
		Logger.Sync()
	}
}

// Structured field validation and standardization functions

// ValidateComponent ensures component name is from allowed set
func ValidateComponent(component string) string {
	allowedComponents := map[string]bool{
		"btree":   true,
		"raft":    true,
		"api":     true,
		"db":      true,
		"storage": true,
		"server":  true,
	}

	if allowedComponents[component] {
		return component
	}

	// Default to "unknown" for invalid components
	return "unknown"
}

// GetStructuredLogger creates a logger with standard component field
func GetStructuredLogger(component string) *zap.Logger {
	logger := GetLogger()
	validatedComponent := ValidateComponent(component)
	return logger.With(zap.String("component", validatedComponent))
}

// CreateOperationLogger creates a logger with operation context
func CreateOperationLogger(component, operation string, additionalFields ...zap.Field) *zap.Logger {
	validatedComponent := ValidateComponent(component)

	fields := []zap.Field{
		zap.String("component", validatedComponent),
		zap.String("operation", operation),
	}

	// Add any additional fields
	fields = append(fields, additionalFields...)

	return GetLogger().With(fields...)
}

// Standard field creators for consistency
func CorrelationID(id string) zap.Field {
	return zap.String("correlation_id", id)
}

func NodeID(id string) zap.Field {
	return zap.String("node_id", id)
}

func Duration(duration string) zap.Field {
	return zap.String("duration_ms", duration)
}

// DurationFromTime calculates duration in milliseconds from a start time
func DurationFromTime(start time.Time) zap.Field {
	ms := float64(time.Since(start).Nanoseconds()) / 1000000
	return zap.String("duration_ms", fmt.Sprintf("%.2f", ms))
}

// HTTPRequestLogger creates a logger for HTTP requests with common fields
// Note: Deprecated - use embedded component loggers instead
func HTTPRequestLogger(component, operation string, r *http.Request) *zap.Logger {
	return CreateOperationLogger(component, operation,
		zap.String("remote_addr", r.RemoteAddr),
		zap.String("user_agent", r.UserAgent()))
}

func PageID(id string) zap.Field {
	return zap.String("page_id", id)
}

func Term(term int64) zap.Field {
	return zap.Int64("term", term)
}

func PeerID(id string) zap.Field {
	return zap.String("peer_id", id)
}

// Helper for error logging with context
func LogErrorWithContext(logger *zap.Logger, msg string, err error, fields ...zap.Field) {
	allFields := append(fields, zap.Error(err))
	logger.Error(msg, allFields...)
}

// Test log file management utilities

// CleanupTestLogs removes test log files older than specified duration
func CleanupTestLogs(logDir string) error {
	if logDir == "" {
		logDir = "."
	}

	// Find all test log files
	files, err := os.ReadDir(logDir)
	if err != nil {
		return err
	}

	for _, file := range files {
		if strings.HasSuffix(file.Name(), "_test.log") {
			filePath := fmt.Sprintf("%s/%s", logDir, file.Name())

			// Get file info
			info, err := file.Info()
			if err != nil {
				continue
			}

			// Remove files older than 24 hours
			if info.ModTime().Add(24 * 60 * 60 * 1000 * 1000 * 1000).Before(time.Now()) {
				os.Remove(filePath)
			}
		}
	}

	return nil
}

// GetTestLogPath returns the appropriate log path for test context
func GetTestLogPath() string {
	isTest, testName := detectTestContext()
	if isTest && testName != "" {
		return fmt.Sprintf("./%s.log", testName)
	}
	return ""
}
