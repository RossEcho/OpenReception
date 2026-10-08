# AI-guided setup contract

This document is for an AI coding assistant helping a person install OpenReception. Read the repository before acting, explain every user-side step in plain language, and verify local results whenever tools are available.

## Safety rules

1. Never ask the user to paste an access token, API key, Meta App Secret, webhook verification token, tunnel token, panel password or cookie-signing secret into chat.
2. Ask the user to enter secrets directly into their local `.env` file or local System settings page, then report only whether validation passed.
3. Never print `.env`, Docker environment output, authorization headers, secret-bearing URLs or `/data/state.json`.
4. Never commit `.env`, runtime state, uploaded assets, databases, logs or customer data.
5. Use only Meta WhatsApp Cloud API. Do not add Messenger, Instagram, Threads or another messaging product.
6. Never delete a Docker volume or overwrite an existing `.env` without explicit approval and a verified backup.
7. Clearly distinguish the control-panel password from the four-digit WhatsApp admin PIN.
8. Treat a Quick Tunnel URL as temporary. Do not describe it as production-ready.

## Opening message

Start with this promise:

> I’ll guide you through Docker, the private environment file, Meta’s WhatsApp webhook, OpenRouter and the first business configuration. I will never ask you to send secret values here. When a secret is needed, I’ll tell you exactly where to enter it locally, then verify only whether the service reports it as valid.

## Phase 1 — machine readiness

Explain that Git and Docker Compose v2 are required. Check without installing anything silently:

```powershell
git --version
docker version
docker compose version
```

If Docker is unavailable, ask the user to start Docker Desktop. If installation is necessary, use the official Docker instructions for the user's operating system.

## Phase 2 — project and private configuration

```powershell
git clone https://github.com/RossEcho/OpenReception.git
Set-Location OpenReception
Copy-Item .env.example .env
```

Tell the user to open `.env` locally. Explain the purpose—not the value—of each required field:

- `WEBHOOK_VERIFY_TOKEN`: a new random value chosen locally and entered again in Meta webhook configuration.
- `META_APP_SECRET`: Meta App Dashboard → App settings → Basic.
- `WHATSAPP_WABA_ID`: the WhatsApp Business Account identifier.
- `WHATSAPP_PHONE_NUMBER_ID`: the Cloud API phone identifier, not the visible phone number.
- `WHATSAPP_ACCESS_TOKEN`: a test token for development or suitable long-lived system-user token for production.
- `OPENROUTER_API_KEY`: created in the operator's OpenRouter account.
- `OPENROUTER_MODEL`: an exact supported OpenRouter model slug.
- `CONTROL_PANEL_PASSWORD`: a long unique password.
- `CONTROL_PANEL_SECRET`: at least 32 random bytes used for session signing.

Ask the user to reply only **done** after saving. Never ask them to paste the values.

## Phase 3 — start and verify locally

```powershell
docker compose --profile tunnel up --build -d
docker compose ps
docker compose logs --tail 100 webhook
docker compose logs --tail 100 tunnel
```

Verify that the webhook is healthy, `http://127.0.0.1:3000/health` returns `{"status":"ok"}`, and tunnel logs contain an HTTPS `trycloudflare.com` URL. Do not claim the setup works merely because fields are populated; use the System settings diagnostics to distinguish **configured** from **valid**.

## Phase 4 — Meta Dashboard

Guide the user through these exact tasks, allowing for small Meta wording changes:

1. Open the existing Meta app.
2. Open **WhatsApp → Configuration**.
3. Under Webhooks, choose **Edit** or **Configure webhook**.
4. Enter `https://<current-public-host>/webhook/whatsapp` as Callback URL.
5. Enter the locally stored `WEBHOOK_VERIFY_TOKEN` as Verify token.
6. Verify and save.
7. Subscribe the WhatsApp webhook to `messages`.
8. Ensure the correct WhatsApp Business Account is subscribed to the app.

Explain that a Quick Tunnel address changes when recreated. Production needs a named tunnel or another stable HTTPS reverse proxy.

## Phase 5 — System settings

Open `http://127.0.0.1:3000/admin`, then System settings. Explain:

- **WhatsApp:** identifiers and credentials for inbound and outbound messages.
- **OpenRouter:** primary/fallback model, endpoint, timeout, output limit and temperature.
- **Cloudflare:** optional named-tunnel token and stable hostname.
- **Diagnostics:** live validation of credentials and public callback reachability.
- **Panel preferences:** dashboard language and appearance only.

Existing secrets are deliberately never displayed again. Blank secret inputs preserve their current values. Recreate containers after integration changes because environment values load during startup.

## Phase 6 — Bot settings

Explain every group:

- **Business name/type:** identity and conversational context.
- **Admin phone:** number allowed to enter WhatsApp admin mode.
- **Owner contact:** handoff phone, disclosure toggle and notification toggle.
- **Business hours:** customer information and scheduling context.
- **Pricing:** customer-visible prices.
- **Service durations:** minutes used for estimates and conflict checks.
- **Products/services:** supported offerings and explanations.
- **Additional information:** general factual knowledge.
- **Only disclose when asked:** conditional facts that must not be volunteered.
- **Assistant instructions:** tone and business behavior, subordinate to safety rules.
- **Daily AI replies:** per-user generation budget.
- **Default appointment duration:** fallback when no service duration matches.
- **Reminder/admin notice:** notification lead times.
- **Assets:** images plus a natural-language instruction describing when each is relevant.

Tell the user the tutorial is skippable and all settings remain editable. Bot settings are also available through authenticated admin chat; System settings are dashboard-only.

## Phase 7 — admin PIN and smoke test

The owner sends `Admin` from the configured admin number. First use asks the owner to create a four-digit PIN. Later sessions require it and last one hour.

Guide a supervised test of:

1. first greeting and preferred-name capture;
2. business-information and price questions;
3. booking with service, relative date and time;
4. five-minute hold and confirmation;
5. appointment lookup and cancellation confirmation;
6. availability without another customer's identity;
7. admin login, schedule lookup and appointment movement;
8. owner handoff and notification;
9. opt-in, broadcast preview and unsubscribe;
10. prompt-manipulation and unrelated-message handling.

Never create a real broadcast, poll or customer notification without explicit operator approval.

## Completion report

Report only non-secret status:

- containers running and healthy;
- local health endpoint working;
- public webhook reachable;
- Meta webhook verified and `messages` subscribed;
- WhatsApp diagnostic valid;
- OpenRouter diagnostic valid;
- first inbound/outbound message result;
- remaining production steps such as stable hostname, permanent token, approved templates, backups and privacy review.

When something fails, identify the failing layer and the next safe diagnostic. Do not recommend regenerating every credential unless evidence identifies that credential.
