# OpenReception

OpenReception is a nonprofit-led, open-source WhatsApp receptionist for appointment-based small businesses. The Dockerized Go service combines natural multilingual conversations through OpenRouter with deterministic scheduling, permission, availability and campaign workflows.

The project began with beauty businesses in mind, but its business identity, services, pricing, hours, policies and assistant behavior are configurable for other appointment-based operations.

## Requirements

- Windows, macOS or Linux capable of running Docker;
- Docker Desktop or Docker Engine with Docker Compose v2;
- approximately 1 CPU core, 512 MB RAM and persistent storage for a small deployment;
- a Meta app with only the WhatsApp product and WhatsApp Cloud API configured;
- a WhatsApp Business Account ID, Phone Number ID, access token and Meta App Secret;
- an OpenRouter account and API key;
- an HTTPS callback URL; the included Cloudflare Quick Tunnel is sufficient for testing;
- outbound HTTPS access to Meta Graph API and OpenRouter.

Local development without Docker requires Go 1.26 or the version declared in [`go.mod`](go.mod).

## Clean installation

```powershell
git clone https://github.com/RossEcho/OpenReception.git
Set-Location OpenReception
Copy-Item .env.example .env
```

Edit `.env` locally and replace every required placeholder. Never commit `.env` or paste its access tokens, API keys, app secret, webhook token, tunnel token, panel password or signing secret into an issue or chat.

For an AI-guided installation, give [`AGENTIC_SETUP.md`](AGENTIC_SETUP.md) to a coding assistant. Its instructions walk a nontechnical operator through Docker, Meta, OpenRouter, diagnostics and first-use configuration without asking them to disclose secrets.

## Running services

```powershell
docker compose --profile tunnel up --build -d
docker compose ps
docker compose logs -f webhook
```

The webhook is available locally at `http://127.0.0.1:3000/webhook/whatsapp`. The control panel is at `/admin` on the same host or HTTPS tunnel.

Persistent schedules, customers, settings, PIN data, usage counters, and reminder state are stored in the `assistant_data` Docker volume as `/data/state.json`.

## Control panel

Open:

```text
https://<current-tunnel-host>/admin
```

Dashboard authentication is controlled by `CONTROL_PANEL_AUTH_ENABLED` and is enabled in `.env.example`. During local development it may be set to `false`; password-free access is then limited to `127.0.0.1`, `::1` and `localhost`, while public tunnel requests remain protected. Keep it enabled in production. This password is separate from the owner chat PIN.

The first visit includes a skippable onboarding slideshow. All configuration remains available later. The control panel uses separate Main, Engagement, Bot settings, and System settings pages with a responsive sidebar. It contains:

- upcoming appointments with date, time and status controls;
- customer, message, appointment and AI-call analytics;
- business name and type;
- admin WhatsApp phone number;
- customer-visible pricing plus internal per-service duration estimates, products, hours and additional information;
- assistant behavior instructions;
- appointment duration, reminder timing and daily AI limits.
- JPEG/PNG customer assets with a natural-language “when to use” instruction.
- polls with 2–10 fixed answers, open-answer questionnaires, private response history, and secure-random lottery draws from subscribers or campaign respondents.
- light and dark appearances plus English and Hebrew panel languages with RTL layout;
- a protected environment editor for WhatsApp Cloud API, OpenRouter, and Cloudflare named-tunnel settings.

System settings writes approved integration fields to the mounted `.env` file. Existing access tokens, API keys, app secrets, webhook tokens, and tunnel tokens are never rendered into HTML; leaving a secret field blank preserves its current value. Integration changes require `docker compose --profile tunnel up -d` (or the production named-tunnel profile) to recreate/restart services. Panel appearance and language preferences are stored in application state and apply immediately. Quick Tunnels do not require Cloudflare credentials; the Cloudflare fields are for the later production named tunnel.

Assets are stored under `/data/assets`, uploaded to WhatsApp Cloud API media, and selected during the existing intent-classification call. An automatically selected asset is sent at most once per customer. If the customer explicitly asks to see or resend it, the asset may be sent again. Media is refreshed from the local copy before the stored WhatsApp media reference ages out.

## Customer chat skills

Every customer message is classified privately by the model as information, start/resume scheduling, a booking answer, appointment lookup, cancellation, final approval, or leaving the flow. There are no customer-facing command keywords. Go still validates and executes all scheduling and ownership actions deterministically.

Known customer names, phone numbers, email addresses, and developer contact details are replaced with opaque placeholders before OpenRouter calls and restored locally in the returned output.

Developer identity, contact details, and a public profile URL are code-managed through the server's `developer-set` command and are intentionally absent from the owner dashboard. They are disclosed only when a customer explicitly asks who created/developed the assistant or requests the developer's details; ordinary creator questions are not treated as prompt manipulation.

Owner-contact handoff is also configurable in Settings. The owner number can remain private, in which case a contact request always sends the owner the customer's saved name and WhatsApp number and confirms the handoff only after delivery succeeds. If sharing is enabled, the number is returned to the customer; a separate toggle controls whether the owner is also notified.

Admins can ask a customer to reschedule by appointment ID and include an optional personal note. The original customer appointment becomes a rescheduling request, its former time is retained as an anonymous owner block, and the customer receives an apology with instructions to choose a new time. Customer availability searches offer the nearest free time for the selected service or search a requested date. Availability responses reveal only free/taken status and never another customer's identity or contact details.

An authenticated chat admin can prepare a mass message with `broadcast: message`. The assistant previews subscribed, declined, and not-yet-consented counts and requires `confirm broadcast` before sending; `cancel broadcast` discards it. Immediately after a customer's first confirmed appointment, the assistant asks once whether they want occasional WhatsApp updates and offers. Only an explicit yes makes that customer eligible. Customers can naturally unsubscribe or subscribe again. Broadcasts never go to the admin number, and the customer's choice affects only promotional and general update broadcasts—not appointment confirmations, reminders, cancellations, or rescheduling notices. Free-form WhatsApp messages can be rejected by Meta outside the 24-hour customer-service window; production campaigns to older contacts require an approved WhatsApp message template and appropriate customer consent.

Conversation inference uses a rolling one-hour derived context, capped at 20 events. It stores and sends only structured intent, canonical services, outcome type, and booking stage—never previous customer or assistant message text. Recent-service shortcuts expire with the same one-hour window. A request to recall a detail unavailable in that context receives an outside-memory response in the customer's locked language.

The full business name is used only in the first-ever customer greeting. Further messages do not repeat a greeting; the first successful reply in each new 24-hour window begins with a natural welcome-back greeting.

An information question during scheduling is answered without destroying the unfinished booking. When the customer later asks to continue scheduling, the assistant returns to the pending question. After service, date, time, and name are collected, the selected slot is held for five minutes while awaiting final approval; an unapproved hold then expires.

Internal service durations guide customer time estimates and deterministic conflict checks without automatically appearing in price-only answers. Response-generation calls are limited per customer per local calendar day by `Daily AI response calls / user`; required internal intent-classification calls are tracked separately in analytics.

## Owner chat skill

Set the owner phone in the control panel using international digits, for example `9725...`.

The configured owner phone uses the normal customer assistant by default. Sending exactly `Admin` switches to owner authentication and prompts for the four-digit PIN. The PIN is salted and iteratively hashed; it is never stored in plain text. Admin sessions remain active for one hour, and five incorrect attempts lock PIN entry for 15 minutes. Send `exit admin` to return to customer mode early.

Authenticated owner commands:

```text
schedule
move ID YYYY-MM-DD HH:MM
cancel ID
```

The owner can also write naturally in any language to create a poll, send an open questionnaire, close a campaign, or draw a lottery winner. The model interprets the request and multilingual customer replies, while Go validates recipients, stores answers, closes superseded campaigns, and performs winner selection with a cryptographically secure random source. Only customers who explicitly opted into general messages are eligible. One campaign is active at a time, optional existing assets can be attached, and the winner is never messaged automatically.

Moving or cancelling through owner chat notifies the affected customer. New bookings notify the configured owner. The owner also receives an upcoming-appointment notice according to the dashboard setting.

Authenticated admin chat and the control panel share the same action layer. The owner may use natural language to view or update every Bot setting, inspect dashboard analytics and customers, view/move/complete/cancel appointments, request rescheduling, prepare broadcasts, create or close engagement campaigns, and draw lotteries. Customer-facing assets can be added by sending a JPEG/PNG during an active admin session with a caption containing the asset name and when it should be used; existing assets can also be removed by name. The model converts open-language requests into typed actions, while Go validates and executes the actual mutation. System settings—WhatsApp, OpenRouter, Cloudflare, panel appearance, and panel language—remain dashboard-only by design.

## Reminders

The Go reminder worker runs every minute:

- customers receive a deterministic reminder at the configured number of hours before an appointment;
- the owner receives an upcoming notice at the configured number of minutes;
- reminder state is persisted so container restarts do not resend completed reminders.

WhatsApp conversation-window and template rules still apply to outbound reminders. A production deployment should use approved templates when reminders fall outside the service window.

## Environment

Copy `.env.example` to `.env`; the example contains the complete supported environment-variable reference with non-secret placeholders. Important runtime fields are:

```dotenv
DATA_FILE=/data/state.json
ASSET_DIR=/data/assets
BUSINESS_TIMEZONE=Asia/Jerusalem
ADMIN_PHONE_NUMBER=
CONTROL_PANEL_PASSWORD=<random-local-password>
CONTROL_PANEL_SECRET=<random-cookie-signing-secret>
CONTROL_PANEL_COOKIE_SECURE=true
```

`ADMIN_PHONE_NUMBER` seeds a new data store only. Once the store exists, edit the admin phone in the control panel.

## Testing

```powershell
$env:GOCACHE="$PWD\.cache\go-build"
go test ./...
.\scripts\test-local-webhook.ps1
```

The test suite covers deterministic booking, initial admin PIN creation, persisted one-hour sessions, OpenRouter fallback, WhatsApp request behavior, model-interpreted admin engagement requests, polls, questionnaires, opt-in filtering, response capture, and lottery eligibility.

## Data and machine migration

Persistent business state is stored in the Docker volume `assistant_data`: `/data/state.json` contains settings, customers, appointments, PIN hash and counters, while `/data/assets` contains uploaded media.

For a clean installation on another machine, move only the Git repository and create a new `.env`. To migrate an active business, back up `.env` and the `assistant_data` volume outside Git, install Docker on the destination, restore both privately, and start Compose. Both backups are sensitive because they contain credentials or customer records.

The repository intentionally excludes `.env`, databases, runtime state, uploaded assets, caches and logs.

## Current test tunnel

Cloudflare Quick Tunnels are temporary. Find the current URL with:

```powershell
docker compose logs tunnel
```

If the hostname changes, update the callback URL in Meta Dashboard and verify it again. For production, use a named tunnel or stable HTTPS domain.

### Permanent named tunnel

Create a remotely managed tunnel in Cloudflare and publish a hostname such as `whatsapp.example.com` to `http://webhook:3000`. Put its `eyJ...` token in `.env` as `CLOUDFLARE_TUNNEL_TOKEN` and record the hostname as `CLOUDFLARE_PUBLIC_HOSTNAME`. Start it with `docker compose --profile named-tunnel up -d named-tunnel`. The token is passed through the `TUNNEL_TOKEN` environment variable rather than exposed in the process command. After verifying the hostname, use `https://<hostname>/webhook/whatsapp` as the Meta callback URL and stop the Quick Tunnel service.

## Support and developer

Created and maintained by **Ross Masyukov**.

- WhatsApp/support: `+972 50-448-3162`
- LinkedIn: [Rostislav Masyukov](https://www.linkedin.com/in/rostislav-masyukov-728040171/)
- Bugs and feature requests: [GitHub Issues](https://github.com/RossEcho/OpenReception/issues)

Never include API keys, access tokens, webhook secrets or customer information in a public issue.

## License

OpenReception is licensed under the **GNU Affero General Public License v3.0** (`AGPL-3.0`). Modified versions offered over a network must make their corresponding source available as required by the license.

OpenReception is not affiliated with or endorsed by Meta, WhatsApp, OpenRouter or Cloudflare. Their names and trademarks belong to their respective owners.
