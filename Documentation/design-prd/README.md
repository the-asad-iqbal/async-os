# AsyncOS — Design PRD

**What this is:** the product requirements for the AsyncOS web app's design: every page, what it must show, what people do on it, and the states it has to handle.
**Who it's for:** the product designer.
**Status:** Draft 1, 16 October 2026.

> **How to read this.** This document says **what** each screen must hold and **how it should behave**. It does not say how it should **look**. Colours, type, spacing, corner radius, shadows, iconography and layout are the designer's call, to match the AsyncOS brand. The screenshots in [`current/`](current/) show today's working prototype. Use them to see what each page contains, not as a style reference: the current look is a placeholder and will be replaced.
>
> Each requirement is tagged:
> - **[Now]**: the controller already provides this data or action, so the UI can be built straight away.
> - **[Next]**: wanted, but needs backend work first. Please design for it, and mark it as phase 2.

---

## 1. Product in one paragraph

AsyncOS is the operating system for a small industrial PC that sits in a site's electrical room. It reads energy meters and solar inverters over Modbus, and adjusts the inverters' output limit every second so that:

1. **Grid export** stays at or below an agreed limit (often zero).
2. **Running gensets** never drop below their minimum load. Below that load they reverse-power, wet-stack, or trip.

Everything is shown in a web app served by the device itself, on the site's local network. It covers live power flow, energy history, devices, alarms, configuration, users and an audit trail. The same app also runs full-screen on a wall-mounted screen (kiosk mode).

Think of it as **Home Assistant's energy dashboard and power-flow card, built for one purpose (PV + grid + genset control) and with our own identity**.

## 2. Goals and success criteria

| Goal | We'll know it works when… |
| --- | --- |
| Anyone can tell what the site is doing at a glance | From across the room, within 3 seconds, a person can say whether the site is on grid, on genset or both, and whether PV is being held back |
| Operators trust the controller | The reason for every curtailment is stated in plain words, next to the number |
| Problems are impossible to miss | A critical alarm is visible on every page, not just on the Alarms page |
| Installers commission a site without a laptop full of tools | Adding a meter, testing it and seeing live readings happens in one flow |
| It works where it's used | Phone in a hot plant room, a 1366×768 panel PC, a 4K wall screen, in light and dark |

**Non-goals:** a public website, cloud or multi-site fleet views (later), a mobile app (the web app must be responsive instead), and a plugin or add-on system.

## 3. Users and roles

| Role | Who | What they do | Can see | Can change |
| --- | --- | --- | --- | --- |
| **Viewer** | Site owner, manager, auditor | Checks production and savings | Overview, Energy, Devices, Alarms | Nothing |
| **Operator** | Shift engineer, site technician | Watches the plant, acknowledges alarms, changes the export limit when the utility asks | + same | Acknowledge alarms; export limit, within the bound the installer sets |
| **Installer** | EPC / commissioning engineer | Sets up meters, inverters, gensets and control rules | + Control, Site setup, Audit log | All control and site settings, register tools, restart |
| **Owner** | The account holder (first user) | Everything, plus managing people | + Users | Users, display links, updates |
| **Display** | A wall screen (not a person) | Shows the kiosk view only | Kiosk only | Nothing |

**Design requirements:**
- **R-ROLE-1 [Now]:** Navigation shows only the pages a role can open. There are no locked or greyed-out items.
- **R-ROLE-2 [Now]:** Read-only users still see settings values, shown as read-only text rather than disabled inputs, with a short line saying who can change them.
- **R-ROLE-3 [Now]:** The current user's name and role are always visible, in the sidebar footer or account menu.

## 4. Where it runs (design constraints)

| Constraint | What it means for the design |
| --- | --- |
| **Screens** | Phone (360–430 px), tablet, 1366×768 panel PC (the most common size on site), 1920×1080, 4K wall screens. Touch and mouse. |
| **Kiosk hardware** | Low-power Intel GPUs running a WebKit browser. Avoid heavy blur, backdrop filters, large animated shadows and full-screen gradients that animate. Subtle motion is fine. |
| **Environment** | Bright plant rooms and outdoor phone use. Contrast must hold up, so check WCAG AA at minimum and AAA for key numbers. |
| **Offline** | The device has **no internet**. Fonts, icons and images ship inside the device, so there are no Google Fonts, icon CDNs or remote images. Pick a typeface we can bundle (an OFL licence is ideal). Inter is used today; you are free to choose another. |
| **Implementation** | Plain HTML, CSS and JavaScript, with no React or Tailwind build. Anything designed must be buildable with CSS (grid, flex, custom properties) and SVG. Deliver the design tokens as named values (colour, type scale, spacing, radius, elevation) so they map 1:1 to CSS variables. |
| **Themes** | Light and dark are both required. They follow the system setting today; a manual toggle would be nice to have. Kiosk defaults to dark. |
| **Numbers first** | This is an instrument. Use tabular figures for all live numbers, and keep units visually lighter than the values. |

## 5. Vocabulary and data conventions

The designer needs these words, because they appear in the UI.

| Term | Meaning |
| --- | --- |
| **PV / Solar** | Solar inverters' AC output |
| **Load / Consumption / Home** | What the site uses: PV + grid import + genset − export |
| **Grid import / export** | Power bought from or sent to the utility. Shown as one signed number: **+ import, − export** |
| **Export limit** | Most power the site may send to the grid. **0 = zero export** |
| **Genset** | Diesel or gas generator. Has a **rating** (kW) and a **minimum load** (% or kW) |
| **Reverse power** | Power flowing *into* a genset. Dangerous; it trips the genset |
| **PV limit (%)** | The output cap the controller sends to all controllable inverters. 100 % = unrestricted |
| **Curtailing** | PV is being held back to protect the grid limit or the genset minimum |
| **Headroom** | How much more PV the site could take right now before hitting a limit |
| **Binding constraint** | Which rule is setting the limit right now: *Export limit* or *Genset minimum* |
| **ATS / source meter** | One meter that sees either grid or genset, through an automatic transfer switch, plus a "genset running" signal saying which |
| **Fail-safe** | A required meter stopped answering, so the controller curtails to a safe level or holds the last limit |
| **Stale** | A reading older than the configured timeout |
| **Monitor only** | The controller reads everything but writes nothing to inverters |

**Units and formatting**
- Power: W below 1 kW, then kW with 1 decimal place, then MW. Energy: Wh, then kWh, then MWh.
- Percentages are whole numbers, except the PV limit, which has 1 decimal place.
- Times follow the site time zone. Live values update every second, so avoid layouts that jump when a number gets a digit longer.

**Controller status.** Every page shows it, so each one needs a distinct, colour-blind-safe treatment (icon + colour + word):

| Status | Meaning | Tone |
| --- | --- | --- |
| **Full output** | No limit active, PV runs freely | Good |
| **Curtailing** | PV held back, which is normal and expected | Informational (not a warning) |
| **Fail-safe** | Required data missing; safety action taken | Critical |
| **Monitoring** | Monitor-only mode; no control | Neutral / caution |
| **No controllable PV** | No inverter that can be limited | Caution |
| **Starting** | First seconds after boot | Neutral |

**Energy-flow colours.** You choose the hues, but give each source **one fixed colour used everywhere**: power flow, charts, KPIs, legends and the kiosk.
`Solar` · `Grid import` · `Grid export` · `Genset` · `Load` · (later) `Battery`

## 6. Site operating modes — grid, genset, export and sync

This is the heart of the product and the area that most needs good UX. A site is always in exactly one **operating mode**. The UI must make that mode, and any change of mode, unmistakable.

| Mode | How we know | What the controller protects | What the user should see |
| --- | --- | --- | --- |
| **Grid only** | Grid voltage present, no genset running | Export limit | Grid as the active source; export vs limit gauge |
| **Genset only (islanded / grid outage)** | Grid voltage absent, one or more gensets running | Genset minimum load; no reverse power | A clear "Grid outage — running on genset" state; genset load vs minimum gauge per genset |
| **Grid + genset in parallel (synchronised)** | Grid present **and** genset running | **Both** limits at once; the tighter one wins | A "Synchronised" badge; both gauges; which one is binding |
| **No source / blackout** | No grid, no genset | Nothing can run | A critical full-width state |
| **Transferring** | ATS switching; source flips within the debounce time | Holds the limit steady | A short-lived "Switching to genset…" state, then the new mode |

**Requirements**
- **R-MODE-1 [Now]:** Show the operating mode prominently on Overview and Kiosk, as a label + icon + colour. Grid present and gensets running are already provided; the mode is derived from them.
- **R-MODE-2 [Now]:** **Export card.** Live grid power as one number with a direction (Importing 12.4 kW / Exporting 0.8 kW), a gauge or bar of export against the export limit, and the safety margin shown as a zone before the limit. When export goes over the limit, it turns critical and shows how long it has been over.
- **R-MODE-3 [Now]:** **Genset card, one per genset.** Running / Stopped; output kW against rating; a bar marked with the **minimum load line**; a reverse-power alert state; and, for an ATS meter, an "On grid / On genset" source tag.
- **R-MODE-4 [Now]:** **"Why" line.** One sentence explaining the current limit, for example *"Holding export at the grid limit"* or *"Keeping Genset 1 above 30 % load"*. It sits next to the PV limit number.
- **R-MODE-5 [Next]:** **Mode timeline.** A strip for today showing grid / genset / parallel / outage periods, like a Gantt bar under the power chart, with hover times.
- **R-MODE-6 [Next]:** **Transfer events** in the alarm and event log: *"10:42 Grid lost → genset started (8 s)"* and *"11:15 Grid restored → genset stopped"*.
- **R-MODE-7 [Next]:** **Genset synchronisation details** when the genset controller exposes them over Modbus: breaker state, synchronising / synchronised, frequency and voltage match. Design a compact "sync panel" (grid V/Hz vs genset V/Hz, phase-match indicator, breaker open/closed) that appears only when a sync-capable genset controller is configured.
- **R-MODE-8 [Next]:** Genset **running hours**, starts today, **fuel level**, and estimated litres saved by solar. Leave room for these in the genset card.

## 7. Information architecture

```
Overview        (home)
Energy          (history)
Devices         (live readings per device)
Alarms
── Configuration (installer, owner)
Control         (control rules)
Site setup      (buses, devices, commissioning tools)
── Administration
Users           (owner)
Audit log       (installer, owner)
Account         (via the user menu)
Kiosk           (full-screen, no navigation)
Setup / Sign in (before login)
```

**Global shell requirements**
- **R-SHELL-1:** Desktop has persistent side navigation, collapsible to icons. Tablet and phone have a top bar with a menu drawer, or a bottom tab bar holding the top 4 pages; designer's call.
- **R-SHELL-2 [Now]:** A global status strip or header showing the site name, operating mode, controller status, Control on / Monitor only, and live connection state (Live / Reconnecting…).
- **R-SHELL-3 [Now]:** Critical alarms show as a banner at the top of **every** page, linking to Alarms, until they are acknowledged or cleared. An alarm count badge sits on the Alarms nav item.
- **R-SHELL-4 [Now]:** A "Kiosk mode" entry point and an account menu (name, role, Account, Sign out).
- **R-SHELL-5:** Design these states for every data area: loading (skeletons), empty, error with a retry, stale data (greyed out, with "last updated 2 min ago"), and offline device.
- **R-SHELL-6:** Feedback: toasts for success, inline errors on fields, and confirmation dialogs for risky actions. Every risky action names its consequence, for example *"Inverters will start receiving limits"*.
- **R-SHELL-7:** Tooltips or info popovers explain every technical term and setting. They must work on touch, so tap to open.

---

## 8. Pages

### 8.1 Sign in and first-time setup  ·  [Now]
Screenshot: `current/setup.png`

- **Setup (first boot only):** the device's screen shows a 12-character **setup code** (like `ABCD-EFGH-JKLM`). The form has fields for the setup code, the owner's name, a username and a password (10+ characters, not the username). A short explanation says where to find the code.
- **Sign in:** username, password and errors. After repeated failures the account is locked and sign-in is refused for a while; the screen must say so plainly, without revealing whether the username exists.
- **Kiosk claim:** a display link opens `/kiosk` without a login.
- Both screens are branded. AsyncOS logo, product name and version go in the footer.

### 8.2 Overview (home)  ·  the most important page
Screenshots: `current/overview.png`, `current/overview-dark.png`, `current/mobile.png`

**Job:** answer *"What is my site doing right now, and is everything OK?"*

Blocks in priority order:

1. **Header [Now]:** site name, operating mode (§6), controller status, Control on / Monitor only, and Live indicator.
2. **Critical alarms [Now]:** banners (see R-SHELL-3).
3. **Power flow [Now]:** an animated diagram of Solar, Grid, Genset and Load (later Battery) in the style of Home Assistant's *Power Flow Card Plus*.
   - Each node shows its live power.
   - Lines animate in the direction energy flows, with speed scaled to power.
   - Grid shows import or export with a direction arrow.
   - The Genset node shows a reverse-power alert state.
   - The Load node has a ring split by source share (how much comes from solar, grid and genset).
   - Nodes hide when the site doesn't have that source.
   - Tapping a node opens that device's detail **[Next]**.
   - Today it is the vendored Power Flow Card Plus re-skinned with our colours. A custom design is welcome. It must be drawable in SVG/CSS and animate smoothly on low-end GPUs.
4. **Controller card [Now]:**
   - Big **PV limit %** with a bar.
   - The *why* line (R-MODE-4).
   - Facts: PV output, controllable capacity, headroom, binding constraint, grid present, gensets running.
   - Operators also get a **"Change export limit"** action, which opens a dialog showing the allowed range.
5. **Export card and genset cards [Now]:** as in R-MODE-2 and R-MODE-3.
6. **Today [Now]:** energy since midnight.
   - **Solar produced** (today's PV yield)
   - **Grid import** and **Grid export**
   - **Genset energy**
   - **Consumption**
   - **Self-consumption %**: the share of solar used on site
   - **Self-sufficiency %**: the share of the load covered by solar
   - **[Next]** cost and savings: tariff × grid import avoided; litres of diesel saved; CO₂ avoided
   - **[Next]** today's **peak PV**, **peak load**, and **energy curtailed**: how much solar was held back (important for showing the controller's cost)
7. **Live power chart [Now]:** the last 15 minutes of Solar, Load, Grid and Genset as lines or areas, with a hover tooltip. **[Next]** a range switcher (15 min / 1 h / 6 h / today) and the mode timeline (R-MODE-5) underneath.
8. **Inverter strip [Now]:** a compact row per inverter showing name, live kW, % of rating, online state and the limit last sent. Today this lives on Devices; please bring a summary to the Overview.

**Mobile:** the same order, stacked. The power flow must stay readable at 360 px wide.

### 8.3 Energy (history)  ·  [Now]
Screenshots: `current/energy.png`, `current/mobile-energy.png`

**Job:** *"Where did my energy come from and go, over a day, week, month or year?"* This is the equivalent of Home Assistant's Energy dashboard.

- **Period controls:** Day / Week / Month / Year tabs, previous / next, a "Today" shortcut and a date picker **[Next]**.
- **Totals:** Solar produced, Grid import, Grid export, Genset, Consumption.
- **Ratios:** Self-consumption % and Self-sufficiency %, each with its formula in plain words (*"50 kWh of 53 kWh used on site"*).
- **Main chart:** stacked bars per hour (day view), per day (week and month) or per month (year). Sources stack above zero: solar used on site, grid import and genset. Grid export goes **below zero**.
- **[Next]:**
  - Production per inverter (a table and a small multiples chart)
  - Genset energy and running hours per genset
  - Curtailed energy
  - Cost and savings
  - Comparison with the previous period (+12 % vs last week)
  - CSV export
- **Sankey / "Energy distribution" [Next]:** a flow diagram of sources → uses for the period (like HA's energy distribution card).

### 8.4 Devices  ·  [Now]
Screenshots: `current/devices.png`, `current/devices-ats.png`

**Job:** *"Is every meter and inverter talking, and what is it reading?"*

- A card (or a row in a dense list view) per device:
  - Name, role (Grid meter, Genset meter, Grid/genset ATS meter, PV inverter), and make and model.
  - Online / Offline / Disabled.
  - A **Verified / Unverified profile** badge. An unverified register map hasn't been tested on real hardware.
  - Controllable / Read only (inverters).
  - Rating.
  - **Live readings:** power, voltage L1–L3, current, frequency, power factor, DC power, imported and exported energy, lifetime yield, limit read back, running signal.
  - Last limit sent or the last error, and an "updated N s ago" line.
- **ATS meter:** an *On grid / On genset* tag plus **Energy by source**: grid import, grid export, genset output, genset reverse.
- **[Next]:**
  - A device detail page with a per-device chart, today's yield per inverter, a communication-quality history (timeouts per hour) and the last 50 errors.
  - Filters by role and status, and a search box (sites can have 30+ inverters).
  - A grid / list toggle.

### 8.5 Alarms  ·  [Now]
Screenshot: `current/alarms.png`

- **Active alarms:** severity (critical or warning), message, device, time raised, and acknowledged by whom and when. Operators and above get an **Acknowledge** button.
- **Alarm types today:**
  - Grid meter offline (fail-safe)
  - Export above limit
  - Genset meter offline
  - Genset reverse power
  - Genset under minimum load
  - Inverter offline
  - Inverter has no limit control
  - No source supplying the site
- **[Next]:** an alarm **history** tab (cleared alarms, duration and transfer events), filters, and notification settings (email / SMS later).

### 8.6 Control (installer)  ·  [Now]
Screenshot: `current/control.png`

**Job:** set the rules the controller follows. Every field needs an explanation, because these settings protect equipment.

| Group | Settings |
| --- | --- |
| **Mode** | Actively limit PV (on) / Monitor only (off). Turning control on asks for confirmation. |
| **Grid** | Export limit (kW, 0 = zero export) · Safety margin (kW) · Grid counts as present above (V) · Export alarm delay (s) |
| **Genset** | Minimum load (% of rating) · Safety margin (kW) · Counts as running above (kW) · Source change debounce (s) |
| **Inverters** | Ramp-up rate (%/s) · Minimum and maximum limit (%) · Minimum change before re-sending (%) · Re-send every (s) |
| **Fail-safe** | Action: *Curtail to fail-safe limit* or *Hold last limit* · Reading counts as stale after (s) · Fail-safe limit (%) |

- Operators see one card: **Export limit setpoint**, editable within the installer's bound. Everyone else sees the settings read-only.
- **UX asks:**
  - A sticky Save / Discard bar that appears only when something has changed.
  - Show **what changes** before saving (a diff of old → new).
  - Validation shown inline.
  - A **simple / advanced** split: grid and genset basics up front, tuning (ramp, re-send, debounce) under Advanced.
  - Show the effect: next to the export limit, a mini diagram of limit and margin; next to the genset minimum, *"= 18 kW on a 60 kW genset"*.

### 8.7 Site setup (installer)  ·  [Now]
Screenshot: `current/site.png`

**Job:** commission the site: say where the meters and inverters are and how to reach them.

- **Site:** name, time zone, and the most an operator may set the export limit to.
- **Communication (buses):** a table of Modbus connections, each with a name, type (Modbus TCP · RTU over TCP gateway · RS-485 serial), address or port, baud and parity for serial, timeout, and the devices on it. Add / edit / delete.
- **Devices:** a table showing name, ID, role, profile (manufacturer → model, grouped; with notes and an *unverified* badge), bus, unit ID, rating and enabled. Add / edit / delete.
  - The device dialog also has: poll interval; invert power sign; for gensets and ATS meters, **minimum load (kW)** and a **running-signal register** (table, address, bit); and a **Test connection** button that shows live readings.
- **Register reader:** read raw registers (bus, unit, table, address, count) and see the values. This is for bringing up a new inverter model.
- A **Save and apply** / **Discard** bar, with a warning when leaving the page with unsaved changes.
- **UX asks:**
  - Turn "add a device" into a **guided flow**: role → brand/model (searchable, with logos later) → connection → test → done.
  - Show a **site diagram** of buses and the devices on them.
  - Make it clear which settings take effect immediately.
- **Library:** 17 built-in profiles today: SMA, Fronius, SolarEdge, KOSTAL, Huawei, Sungrow, Growatt, Eastron, CHINT, Carlo Gavazzi, ABB, Schneider and generic SunSpec. The list will grow to hundreds, so search is required.

### 8.8 Users (owner)  ·  [Now]
Screenshot: `current/users.png`

- **Users table:** name and username, role (with a tooltip on what it can do), Active / Disabled, and last sign-in. Actions: edit role or status, reset password (which ends their sessions), delete (not yourself).
- **Add user:** name, username, role and a temporary password.
- **Display links:** wall screens get a revocable link, shown **once** at creation with a copy button. The list shows name, created and last seen, with Revoke.
- A **role comparison** (a permissions matrix like the one in §3) should be visible when choosing a role.

### 8.9 Audit log (installer, owner)  ·  [Now]
Screenshot: `current/audit.png`

- Columns: time, user (and role), action (sign-in, settings changed, export limit changed, user created, …), target (with details on hover: what changed from → to), result (success or denied) and IP address. A "Load older" button.
- **[Next]:** filters (user, action type, date range) and export.

### 8.10 Account  ·  [Now]
Screenshot: `current/account.png`

- Profile: name, username, role and member since. Change password. Sign out.
- **System:** OS name, version, hostname and uptime. **Restart controller** (installer and owner, with confirmation).
- **[Next]:**
  - **Software updates** (owner): current version, check / upload an update, A/B update progress and rollback notice.
  - **Network settings:** IP, DHCP or static, hostname.
  - **Backup / restore** of the site configuration.
  - Date and time / NTP.
  - Theme toggle.

### 8.11 Kiosk (wall display)  ·  [Now]
Screenshot: `current/kiosk-dark.png`

**Job:** a screen on the wall of the control room, readable from 3–5 m away, left on for months.

- Full screen with no navigation. Brand, site name, operating mode, controller status, and a big clock and date.
- Large power flow, PV limit and reason, export and genset gauges, and Today's energy.
- A footer with the Live / Reconnecting state and the last update time.
- Behaviour: keeps the screen awake, hides the cursor and reloads daily.
- Critical alarms must take over a significant part of the screen and be readable from a distance.
- **[Next]:** rotating panels (power flow → energy today → devices → alarms), set per display link.
- Sizes: design for 1920×1080 first, then check 1366×768 and 3840×2160 (scale, don't just stretch).

---

## 9. Ideas to consider adding (designer input welcome)

- **Daily summary card:** *"Today: 312 kWh solar, 74 % self-sufficient, 0 export violations, genset ran 2 h 10 m"*.
- **Curtailment insight:** "PV held back today: 41 kWh" with a small chart, so owners see the cost of the export limit.
- **Per-inverter yield ranking:** spot a weak or offline inverter.
- **Genset fuel and savings:** litres saved by PV, from the genset's fuel curve.
- **Weather / irradiance strip** (if a sensor is added later).
- **Battery** node and cards: the architecture allows a battery later, so leave room in the flow and the KPIs.
- **Event timeline:** one chronological feed mixing alarms, mode changes and settings changes.
- **Onboarding checklist** after setup: add a bus → add a grid meter → add inverters → set the export limit → turn control on.

## 10. Components the design system needs

Deliver these as a component sheet in light and dark, with states (default, hover, focus, active, disabled, loading, error):

- App shell: sidebar, top bar, mobile nav, account menu, page header with actions
- Status: status pill, operating-mode badge, connection indicator, online/offline dot, verified/unverified badge, role badge
- Alerts: critical banner, warning banner, toast, inline field error, empty state, skeletons
- Data: KPI tile (value + unit + label + optional delta), big number with unit, gauge or bar with limit and margin markers, ratio ring, facts list (label/value), readings grid, data table (sortable, responsive → stacked cards on mobile)
- Charts: live line/area chart with tooltip and legend; stacked bar chart with a negative axis; timeline strip; sparkline. The palette comes from the energy-flow colours.
- Power flow diagram: nodes, flow lines with moving dots, node ring, alert state
- Forms: text, number with unit suffix, select (with groups and search), switch, checkbox, segmented control (Day/Week/Month/Year), date stepper, info tooltip, inline help text
- Overlays: dialog, confirmation dialog (danger variant), side sheet (for device detail and editing on desktop), popover
- Buttons: primary, secondary, ghost, danger, icon-only; sizes S and M
- Copyable secret field (display link), code/register value display (monospace)

## 11. What we need back from the designer

1. **Brand tokens:** colour (light and dark, plus the 6 energy-flow colours and the status colours), type scale, spacing scale, radius, borders, elevation and motion timing, as a table of named values.
2. **Component sheet** (§10).
3. **Page designs** for every page in §8, at desktop (1440 and 1366 wide) and phone (390). Kiosk at 1920×1080. Overview in both themes.
4. **Key flows:** first-time setup; changing the export limit; turning control on; acknowledging an alarm; adding and testing a device; a grid outage → genset → grid restored sequence shown on the Overview.
5. **States:** loading, empty, offline device, stale data, fail-safe, critical alarm and read-only (viewer).
6. The power-flow diagram's **motion spec**: dot speed relative to power, and the idle state.
7. Figma (or similar) with components and auto-layout, so we can map them to CSS.

## 12. Open questions

- Logo and final brand colours: the current ones are placeholders.
- Should the top-level mental model be *"Home"* (like HA) or *"Site"*?
- Bottom tab bar or drawer on mobile?
- Should the kiosk rotate panels or stay on one view?
- Which of the [Next] items are phase 2, and which are phase 3?
