# sbx web page: Mobbin audit and redesign

## Research goal

Find how developer tools present the pattern behind sbx: paste input, run it, then see a verdict per environment with reasons. Use those patterns to audit and redesign the "Will it relay?" page. All icons are MingCute line icons.

## Searches used (web, Mobbin)

1. Developer tool page with a code editor input for pasting raw data, a run button, and a results panel listing checks with pass and fail status icons
2. CI pipeline run results page listing jobs with green check and red cross status icons, failed job expanded to show error log output and summary at top
3. Compatibility matrix table with one row per item and a column per version or environment, cells showing green check, red cross and warning icons
4. Validation report panel listing errors and warnings with severity icons, error code in monospace, plain explanation and a suggested fix for each issue
5. API playground page with an examples dropdown, a large monospace request input, a send button with keyboard shortcut hint, and an empty response panel waiting for a run
6. Crypto blockchain explorer transaction detail page showing transaction id with copy button, status, fee, size, and inputs and outputs listed

## Pattern clusters

**Input with its actions attached.**
- [Exa](https://mobbin.com/screens/a6d5cd9a-8b6c-4bed-8284-9fddce909a53) puts "Try an example", Clear and Run inside the input box.
- [Cohere](https://mobbin.com/screens/ae3bf0d0-0873-430c-bb7a-d436c2fababe) pairs Reset with Run below its form.
- [Browserbase](https://mobbin.com/screens/fb146336-bb9a-49d8-8192-fa52f65fcf5f) has Reset and Copy code under its editor.

Keeping the actions next to the thing they act on removes a hunt across the page.

**A verdict before any detail.**
- [Snowflake](https://mobbin.com/screens/ab418e9e-2245-4b15-8ea7-9de8d32a889b) opens with a one-line run summary ("Succeeded • 1 task ran • 6m 26s").
- [Base44](https://mobbin.com/screens/71d29ab5-bd1b-46a9-a9d7-0932827e94a4) puts an overall verdict first, then "Key issues", then a "Full report".
- [Square](https://mobbin.com/screens/2bf8396f-b5eb-4c32-ad6e-ee71118f27ae) reduces its result to two counts ("Require review 5 / Imported 0").

**A status icon plus a word, never color alone.**
- [Mintlify](https://mobbin.com/screens/31bc9279-f1e6-449c-9143-583e558609b4) and [GitLab](https://mobbin.com/screens/5c384c13-e9f9-45be-8cef-f08bc517ff7f) use pills with an icon and text.
- [Vercel](https://mobbin.com/screens/b9d9cc23-34a1-434c-a4ed-52a2a4f49bb7) uses a colored dot with a word.

**Comparison across columns.**
- [Clockwise](https://mobbin.com/screens/6dfb7e0e-f5aa-43f5-a098-67de4cf6b776) tints one whole column to single it out.
- [Modal](https://mobbin.com/screens/9765e616-5178-4006-8e39-dcc6266e4954) uses small check and cross icons in a dense grid.

**Grouped issues, with the code shown underneath.**
- [Databricks](https://mobbin.com/screens/bc851bab-0aaf-4cb2-a096-d2ccc6990c20) shows severity counts above a list of issues that expand.
- [OpenAI Platform](https://mobbin.com/screens/012bdbf4-f102-496c-95bd-81f4e4fec126) puts the error in monospace inside a label/value detail panel.
- [Neon](https://mobbin.com/screens/edb456df-fade-4f54-b81b-f24790849d92) puts its error in a tinted block, repeats the failing input and offers a recovery action.

**Copyable identifiers in label/value lists.**
- [Resend](https://mobbin.com/screens/150d20dd-d965-4fc1-b750-3e39dda032d4) shows its metadata as a label/value grid with a copyable ID.
- [Kraken](https://mobbin.com/screens/ae5f4277-3a6d-4572-945f-db2e008c7d1f) shortens the middle of transaction IDs and adds a copy icon.

**Empty states that teach.**
- [Postman](https://mobbin.com/screens/61beea5d-7cb2-4eb9-8865-d5b3cc7d39d9) tells you what to do before the first run ("Enter the URL and click Send").

## Audit of the previous page

| # | Problem | Seen in | Fix |
|---|---|---|---|
| 1 | You had to read five stacked rows to learn the outcome | Results | Summary line plus a strip with one icon per version |
| 2 | The same reason repeated up to four times (chain-26 example) | Results | Group rejections by root cause, with version tags |
| 3 | The raw node message came before the plain explanation | Each row | Plain explanation is the title; the raw reason sits underneath in monospace with a copy button |
| 4 | The banner and chips signalled status by color only | Banner, chips | MingCute icon plus a word everywhere |
| 5 | The example picker was detached from the input, with no Paste or Clear | Input | Tool bar inside the input box: Examples, Paste, Clear, Run |
| 6 | No empty or loading state; nothing explained where hex comes from | Results | Empty state with the version strip and three ways to get hex; a spinner per version while checking |
| 7 | The transaction table wrapped on mobile; txids and reasons couldn't be copied | Transactions | Label/value cards, txid shortened in the middle, copy buttons |
| 8 | The policy notes sat at the bottom, far from the split | Results | "Why they split" callout directly under the strip |
| 9 | Input mistakes only showed up after a server round trip | Input | Live detection: "1 transaction", "Package of 2: parent → child", "Line 2 isn't raw transaction hex" |

## What changed

- **Header:** a Bitcoin mark plus a single health line ("5 versions running · regtest 140 blocks"). If a node is down, the line names it and says to run `sbx up`.
- **Input:** one card containing the hex box, the live detection line, a tool bar (Examples, Paste, Clear, ⌘↵, "Check 5 versions") and a collapsible mempool section with a count badge.
- **Results, in this order:**
  1. Headline with icon and one-line summary
  2. Version strip, with the odd ones out tinted and marked
  3. "Why they split"
  4. Mempool setup warnings
  5. Grouped rejection cards
  6. One "Accepted by …" line
  7. Transaction cards
- **States:** empty (teaches the three ways to get hex), loading (each version shows "Checking" with a spinner), error (icon callout), and success.
- **Icons:** 20 MingCute line icons (Apache-2.0, `mingcute_icon@2.9.71`) in one sprite served from the binary, `/icons.svg`. Only path data was kept from the source SVGs.

## Verified

- Desktop dark mode and 375 px light mode, using the `pkg-anchor` and `chain-26` examples.
- No horizontal scroll and no console errors.
- The CSP stays `default-src 'self'`, and there are no external requests.

## Round 2: results-first workbench (2026-10-16)

The first redesign was still felt to be generic: the hex box dominated, the results were hard to read and the layout felt clumsy.

**Direction: a node operator's console, precise and calm.**

**Layout:**
- Two panes, following [LangChain](https://mobbin.com/screens/e2901ba6-d169-4714-865f-73657b3e9ee9) and [Braintrust](https://mobbin.com/screens/bb7a5740-36c6-4740-babf-05f053e5d8d2): setup on the left, the result on the right.
- The left pane leads with **scenarios**, filtered by Single / Package / Fee bumps. Clicking one runs it.
- The hex box is now a small, secondary field. The orange **Check** button stays pinned at the bottom.

**Result pane:**
- A one-sentence verdict comes first, like the bold summary line in [Postman's test results](https://mobbin.com/screens/17ad05ba-4835-4c81-8a18-1223ebc6cd25). Examples: "Knots 29.4.2 rejects it. The other 4 accept." and "Only Core 31.1 accepts it."
- One plain-language line explains why.
- Next comes a **scoreboard**, the signature element: one tall column per version with a large verdict icon. The odd ones out are outlined and labelled. It's inspired by the stat strip in [n8n's run summary](https://mobbin.com/screens/8e2ed125-52a7-457f-9831-caadfc788629).
- **Why** is one list with dividers instead of stacked cards. Raw node messages sit behind a "Node message" toggle.
- **Transaction** facts are a compact table.

**Character:**
- Monospace for versions and small uppercase section labels; sans for the sentences.
- One Bitcoin-orange accent, used only for the brand mark, the Check button and the selected scenario.
- Green and red are reserved for accept and reject.
- Odd ones out use an ink outline rather than a third color.

**Phones:** the panes stack, the scoreboard becomes rows, and the page scrolls to the result after a check.

**Verified:** 960 px light and dark, 375 px light, a single transaction, a package and a fee bump. No horizontal overflow. `node --check` passes and the Go tests pass.
