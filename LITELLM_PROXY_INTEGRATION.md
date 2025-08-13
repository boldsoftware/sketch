# LiteLLM Proxy Integration

## 🎯 Overview

The Kifaru application has been successfully configured to use a custom LiteLLM proxy at `https://lite.bastco.org/v1` instead of the direct OpenAI API. This provides several benefits including cost optimization, request routing, and centralized API management.

## 🔧 Changes Made

### 1. OpenAI URL Configuration (`llm/oai/oai.go`)

**Before:**
```go
OpenAIURL = "https://api.openai.com/v1"
```

**After:**
```go
// Using custom LiteLLM proxy instead of direct OpenAI API
OpenAIURL = "https://lite.bastco.org/v1"
```

### 2. Affected Models

All OpenAI-based models now route through the LiteLLM proxy:

| Model | User Name | Model Name | URL |
|-------|-----------|------------|-----|
| **GPT-4.1** | `gpt4.1` | `gpt-4.1-2025-04-14` | `https://lite.bastco.org/v1` |
| **GPT-4o** | `gpt4o` | `gpt-4o-2024-08-06` | `https://lite.bastco.org/v1` |
| **GPT-4o Mini** | `gpt4o-mini` | `gpt-4o-mini-2024-07-18` | `https://lite.bastco.org/v1` |
| **GPT-4.1 Mini** | `gpt4.1-mini` | `gpt-4.1-mini-2025-04-14` | `https://lite.bastco.org/v1` |
| **GPT-4.1 Nano** | `gpt4.1-nano` | `gpt-4.1-nano-2025-04-14` | `https://lite.bastco.org/v1` |
| **O3** | `o3` | `o3-2025-04-16` | `https://lite.bastco.org/v1` |
| **O4 Mini** | `o4-mini` | `o4-mini-2025-04-16` | `https://lite.bastco.org/v1` |

### 3. Preserved Non-OpenAI Models

Other model providers continue to use their original URLs:

- **Gemini**: `https://generativelanguage.googleapis.com/v1beta/openai/`
- **Together**: `https://api.together.xyz/v1`
- **Fireworks**: `https://api.fireworks.ai/inference/v1`
- **Mistral**: `https://api.mistral.ai/v1`
- **Moonshot**: `https://api.moonshot.ai/v1`
- **LlamaCPP**: `http://localhost:8080/v1`

## 🧪 Tests Added

### 1. Pentesting Tool Tests

**File: `claudetool/nmap_test.go`**
- ✅ `TestNmapTool_ensureNonInteractive`: Tests automatic non-interactive flag injection
- ✅ `TestNmapTool_calculateTimeout`: Tests smart timeout calculation based on scan complexity
- ✅ `TestNmapTool_Run_InputValidation`: Tests input validation and error handling
- ✅ `TestNmapTool_Tool`: Tests tool configuration and schema
- ✅ `TestNmapArgs_Unmarshaling`: Tests JSON unmarshaling of nmap arguments

**File: `claudetool/bash_pentest_test.go`**
- ✅ `TestBashTool_enhancePentestingCommand`: Tests command enhancement for pentesting tools
- ✅ `TestBashTool_insertFlag`: Tests smart flag insertion logic
- ✅ `TestBashTool_looksLikeTarget`: Tests target detection (IP addresses, domains)
- ✅ `TestBashTool_pentestingToolDetection`: Tests detection of 12+ pentesting tools
- ✅ `TestBashTool_nonPentestingCommandsUnchanged`: Tests that normal commands only get env vars

**File: `claudetool/bashkit/pty_test.go`**
- ✅ `TestPTY_Creation`: Tests PTY creation and initialization
- ✅ `TestPTY_SetWinsize`: Tests terminal window size configuration
- ✅ `TestCopyOutputWithTimeout_*`: Tests timeout handling and idle detection
- ✅ `TestIsPTYSupported`: Tests platform PTY support detection

### 2. LiteLLM Proxy Tests

**File: `llm/oai/oai_test.go`**
- ✅ `TestOpenAIURL_UsesCustomLiteLLMProxy`: Verifies URL change
- ✅ `TestOpenAIModels_UseCustomURL`: Tests all OpenAI models use proxy
- ✅ `TestNonOpenAIModels_UseOriginalURLs`: Ensures other providers unchanged
- ✅ `TestModelByUserName_FindsOpenAIModels`: Tests model lookup functionality
- ✅ `TestListModels_IncludesOpenAIModels`: Tests model registry completeness
- ✅ `TestDefaultModel_UsesCustomURL`: Tests default model configuration
- ✅ `TestAPIKeyEnvironmentVariables`: Tests API key environment variable mapping
- ✅ `TestReasoningModels_Identification`: Tests reasoning model detection

## 🚀 Usage Examples

### Using OpenAI Models (via LiteLLM Proxy)

```bash
# All these commands now route through lite.bastco.org
./kifaru -model=gpt4.1 -prompt="Analyze this code"
./kifaru -model=gpt4o -prompt="Write a pentesting script"
./kifaru -model=o3 -prompt="Solve this complex problem"
```

### Using Other Providers (Direct)

```bash
# These still use direct provider APIs
./kifaru -model=gemini-flash-2.5 -prompt="Generate code"
./kifaru -model=together-deepseek-v3 -prompt="Analyze data"
```

### Environment Variables

The application still uses the same environment variables:

```bash
export OPENAI_API_KEY="your-api-key"  # Used with LiteLLM proxy
export GEMINI_API_KEY="your-gemini-key"
export TOGETHER_API_KEY="your-together-key"
# etc.
```

## 🔍 How It Works

1. **Model Selection**: User specifies model (e.g., `gpt4.1`)
2. **URL Resolution**: System looks up model configuration and finds `https://lite.bastco.org/v1`
3. **Request Routing**: OpenAI-compatible request sent to LiteLLM proxy
4. **Proxy Processing**: LiteLLM proxy routes to appropriate provider (OpenAI, Anthropic, etc.)
5. **Response Handling**: Standard OpenAI-format response returned to application

## 🛡️ Benefits

### 1. **Cost Optimization**
- LiteLLM can route to cheaper providers
- Automatic fallback between providers
- Usage tracking and budgeting

### 2. **Reliability**
- Multiple provider fallbacks
- Request retry logic
- Load balancing

### 3. **Flexibility**
- Easy provider switching
- A/B testing different models
- Centralized configuration

### 4. **Monitoring**
- Centralized logging
- Usage analytics
- Performance metrics

## 🧪 Testing Results

All tests pass successfully:

```bash
# Pentesting tool tests
go test ./claudetool/ -v -run TestNmapTool     # ✅ PASS
go test ./claudetool/ -v -run TestBashTool     # ✅ PASS
go test ./claudetool/bashkit/ -v               # ✅ PASS

# LiteLLM proxy tests  
go test ./llm/oai/ -v                          # ✅ PASS

# Full application build
go build ./cmd/sketch                          # ✅ SUCCESS
```

## 🔧 Configuration

### LiteLLM Proxy Setup

Your LiteLLM proxy at `https://lite.bastco.org/v1` should be configured to:

1. **Accept OpenAI-format requests** on `/v1/chat/completions`
2. **Handle authentication** via `Authorization: Bearer <token>` headers
3. **Route requests** to appropriate providers based on model names
4. **Return OpenAI-format responses** for compatibility

### Model Mapping

The proxy should map model names to actual providers:

```yaml
# Example LiteLLM config
model_list:
  - model_name: gpt-4.1-2025-04-14
    litellm_params:
      model: openai/gpt-4.1-2025-04-14
      api_key: env/OPENAI_API_KEY
      
  - model_name: gpt-4o-2024-08-06  
    litellm_params:
      model: openai/gpt-4o-2024-08-06
      api_key: env/OPENAI_API_KEY
```

## 🎉 Summary

✅ **LiteLLM proxy integration complete**  
✅ **All OpenAI models route through `https://lite.bastco.org/v1`**  
✅ **Other providers unchanged**  
✅ **Comprehensive test coverage added**  
✅ **Backward compatibility maintained**  
✅ **Application builds and runs successfully**

The integration provides a robust foundation for centralized LLM management while maintaining full compatibility with the existing Kifaru architecture.