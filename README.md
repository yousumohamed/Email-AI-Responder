# AI Email Auto-Responder in Go

A production-ready, secure AI Email Auto-Responder application built with Go (Golang). The application monitors a Gmail inbox using IMAP, generates contextual, professional email responses via OpenRouter API, enforces safety and security policies, and dispatches approved replies using Gmail SMTP.

---

## Security Architecture & Separation of Concerns

There is a **strict separation between AI text generation and email transmission**:

```
                    Gmail Inbox
                        │
                        │ IMAP (TLS 993)
                        ▼
                ┌─────────────────────────────────┐
                │        Go Application           │
                │                                 │
                │  - IMAP Reader (INBOX)          │
                │  - Duplicate Check (SQLite)     │
                │  - Email Filtering (Anti-Loop)  │
                │  - Body Parser (HTML/Text)      │
                └───────────────┬─────────────────┘
                                │ (Untrusted email content ONLY)
                                ▼
                ┌─────────────────────────────────┐
                │         OpenRouter API          │
                │                                 │
                │  - Role: System (Instructions)  │
                │  - Role: User (Email content)   │
                │  - Generates text ONLY          │
                └───────────────┬─────────────────┘
                                │ (Raw AI response text)
                                ▼
                ┌─────────────────────────────────┐
                │    Safety / Policy Validator    │
                │                                 │
                │  - Secret leakage checks        │
                │  - System prompt leakage checks │
                │  - False action claims checks   │
                │  - Command/tool syntax checks   │
                │  - Length and emptiness checks  │
                └───────────────┬─────────────────┘
                                │ (Validated text)
                                ▼
                ┌─────────────────────────────────┐
                │       Go Sending Decision       │
                │                                 │
                │  AUTO_REPLY_ENABLED == true &&  │
                │  REQUIRE_APPROVAL == false?     │
                └───┬─────────────────────────┬───┘
               No   │                     Yes │
                    ▼                         ▼
         ┌─────────────────────┐   ┌─────────────────────┐
         │ Terminal Preview    │   │ Gmail SMTP          │
         │ (Sending Disabled)  │   │ (STARTTLS 587)      │
         └─────────────────────┘   └──────────┬──────────┘
                                              │
                                              ▼
                                         Email Reply
```

### Key Security Principles:
- **Zero-Access AI**: The AI model has **no** credentials, **no** access to IMAP/SMTP, and **no** access to the filesystem.
- **Credential Quarantine**: Your Gmail App Password and OpenRouter API keys are never included in the AI prompt, never printed to logs, and monitored by the validator to prevent leakage.
- **Untrusted Input Quarantine**: Incoming customer emails are treated as untrusted data in the `user` role; prompt injections inside incoming emails cannot override system instructions.
- **The AI Never Decides to Send**: The Go application strictly enforces the sending decision based on configuration flags and validation checks.

---

## Requirements

- **Go 1.22+**
- **Gmail Account** with 2-Step Verification enabled
- **Gmail App Password** (Standard passwords **cannot** and **must not** be used)
- **OpenRouter API Key**

---

## Gmail App Password Setup

Google requires an **App Password** for third-party IMAP and SMTP access. Your standard Google account password will **NOT** work.

1. Go to your Google Account: [https://myaccount.google.com/](https://myaccount.google.com/)
2. Navigate to **Security**.
3. Under *How you sign in to Google*, ensure **2-Step Verification** is turned ON.
4. Search for or navigate to **App passwords**: [https://myaccount.google.com/apppasswords](https://myaccount.google.com/apppasswords)
5. Create a new App Password:
   - App Name: `ai-email-responder`
   - Click **Create**.
6. Google will generate a 16-character password (e.g. `abcd efgh ijkl mnop`).
7. Copy this 16-character string and set it as `GMAIL_APP_PASSWORD` in your `.env` file (spaces can be omitted or kept).

### Gmail IMAP & SMTP Settings
- **IMAP Server**: `imap.gmail.com` | **Port**: `993` | **Security**: TLS
- **SMTP Server**: `smtp.gmail.com` | **Port**: `587` | **Security**: STARTTLS

> **Note**: Make sure **IMAP access** is enabled in your Gmail settings:
> Gmail Settings > *Forwarding and POP/IMAP* > *IMAP access* > *Enable IMAP* > *Save Changes*.

---

## Installation & Configuration

### 1. Clone or Open the Repository
```bash
cd ai-email-responder
```

### 2. Configure Environment Variables
Copy the example file to `.env`:
```bash
cp .env.example .env
```
*(On Windows PowerShell: `Copy-Item .env.example .env`)*

Edit `.env` and provide your credentials:

```ini
# Application
APP_NAME=ai-email-responder
APP_ENV=development
LOG_LEVEL=info
POLL_INTERVAL_SECONDS=30

# Gmail Account
GMAIL_EMAIL=your-email@gmail.com
GMAIL_APP_PASSWORD=your-16-character-app-password

# IMAP
IMAP_HOST=imap.gmail.com
IMAP_PORT=993
IMAP_MAILBOX=INBOX

# SMTP
SMTP_HOST=smtp.gmail.com
SMTP_PORT=587

# OpenRouter
OPENROUTER_API_KEY=your-openrouter-api-key
OPENROUTER_MODEL=google/gemini-2.5-flash
OPENROUTER_BASE_URL=https://openrouter.ai/api/v1

# AI Configuration
AI_SYSTEM_INSTRUCTION_FILE=prompts/system.txt
AI_TEMPERATURE=0.3
AI_MAX_TOKENS=500

# Auto Reply Modes
AUTO_REPLY_ENABLED=false
REQUIRE_APPROVAL=true
MAX_EMAILS_PER_RUN=10

# Safety
IGNORE_OWN_EMAILS=true
IGNORE_NO_REPLY_EMAILS=true
IGNORE_AUTOMATED_EMAILS=true
MAX_EMAIL_BODY_LENGTH=10000

# Reply Identity
REPLY_NAME=Email Assistant
```

---

## Operational Modes

### 1. Development Mode (Default & Safe)
```ini
AUTO_REPLY_ENABLED=false
REQUIRE_APPROVAL=true
```
- The application monitors your inbox and fetches new emails.
- The AI drafts an email reply.
- The reply undergoes complete safety and policy validation.
- **The email is NOT sent.**
- The proposed reply is displayed directly in the terminal for human review:
  ```text
  ==============================
  PROPOSED EMAIL REPLY
  ==============================
  To: customer@example.com
  Subject: Re: Website Service

  Hello,

  Thank you for reaching out...
  ==============================
  Sending disabled.
  ```

### 2. Production Mode (Automatic Sending)
```ini
AUTO_REPLY_ENABLED=true
REQUIRE_APPROVAL=false
```
> [!WARNING]
> In production mode, incoming emails passing safety validation will automatically receive a reply via Gmail SMTP. Ensure you thoroughly test prompts and filtering rules in Development Mode first.

Both `AUTO_REPLY_ENABLED=true` AND `REQUIRE_APPROVAL=false` are strictly required. If either setting is unsafe, sending is automatically disabled.

---

## Running the Application

### Download Dependencies
```bash
go mod tidy
```

### Run the Application
```bash
go run .
```

### Build a Standalone Binary
```bash
go build -o bin/ai-email-responder .
./bin/ai-email-responder
```

---

## Running Unit Tests

Run all unit tests across all packages:
```bash
go test -v ./...
```

The test suite covers:
- **Configuration loading, validation, and secret masking** (`config/config_test.go`)
- **SQLite duplicate email tracking and status persistence** (`storage/processed_test.go`)
- **HTML stripping, subject formatting, recipient routing, and email filtering** (`email/parser_test.go`, `email/filter_test.go`)
- **OpenRouter request assembly, rate limit handling, and prompt isolation** (`ai/openrouter_test.go`)
- **Safety validator policy checks** (credential leaks, system leaks, false action claims, tool commands) (`safety/validator_test.go`)

---

## Project Structure

```
ai-email-responder/
├── main.go               # Application entry point, CLI banner, polling & shutdown
├── go.mod                # Module definitions & dependencies
├── go.sum                # Cryptographic checksums of dependencies
├── .env.example          # Environment variable template
├── .gitignore            # Git ignore file (excludes secrets, binaries, logs, db)
├── README.md             # Complete user guide and architecture reference
├── config/
│   ├── config.go         # Environment loading, defaults, and secret-safe validation
│   └── config_test.go    # Configuration unit tests
├── email/
│   ├── types.go          # Email and Reply structures
│   ├── reader.go         # Gmail IMAP client (TLS, unread fetch, MIME parser)
│   ├── parser.go         # HTML-to-text sanitizer, Re: subject & recipient resolver
│   ├── sender.go         # Gmail SMTP client (STARTTLS, RFC 5322 compliance)
│   ├── parser_test.go    # Parser and sanitizer unit tests
│   └── filter_test.go    # Anti-loop and automated email filter tests
├── ai/
│   ├── types.go          # OpenRouter request/response models
│   ├── openrouter.go     # OpenRouter HTTP client with error handling
│   └── openrouter_test.go# OpenRouter mock server unit tests
├── safety/
│   ├── validator.go      # Policy & safety validator (secret leak, tool syntax checks)
│   └── validator_test.go # Safety validator unit tests
├── storage/
│   ├── processed.go      # SQLite persistence for duplicate prevention
│   └── processed_test.go # SQLite storage unit tests
├── prompts/
│   └── system.txt        # Strict system prompt isolating the AI model
└── logs/                 # Dedicated logs directory
```

---

## Graceful Shutdown

Press `CTRL+C` (`SIGINT` or `SIGTERM`) in the terminal. The application will:
1. Stop polling for new messages.
2. Complete any safe in-progress email processing.
3. Close the IMAP connection.
4. Close the SQLite database connection.
5. Exit cleanly.
