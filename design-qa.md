# 模型生态留白分章 — 2026-09-13

final result: passed

## Scope and verification

- The immersive hero remains intact and ends before the ecosystem section begins; there is no overlap between the two regions.
- The ecosystem section now opens with an independent editorial heading and a measured white-space interval, followed by a single restrained 24px-radius panel.
- Replaced the repeated robot portrait with `web/default/src/assets/tokenfly-model-network.jpg`: a generated, no-text abstract model-routing image (1448 × 1086 JPEG, approximately 215KB).
- The panel retains only project-grounded capabilities: OpenAI compatibility, multi-protocol compatibility and usage visibility. The model-gallery route remains the existing real route.
- Added the translated heading “Many models. One interface.” in all seven shipped locales, including zh-TW; the i18n sync report has zero missing keys in every locale.
- Desktop browser inspection at `http://localhost:3001/` confirmed the clean hero boundary, white-space transition, unstacked panel and readable Chinese copy. The 820px and 560px CSS layouts keep the section in a single-column order; an exact physical-phone capture remains outside this focused follow-up.
- `bun run typecheck`, touched-file oxlint, touched-file formatting, production build and `git diff --check` passed.

---

# 快速接入轻量化 — 2026-09-13

final result: passed

## Scope and verification

- Removed the oversized rounded black wrapper from the quick-start section. It now continues on the shared warm-white page surface with the existing editorial grid.
- The deep graphite treatment is intentionally limited to the runnable code preview, keeping it as the section’s only dark visual anchor.
- Steps retain divider-led hierarchy, hover/focus/active feedback and keyboard-accessible controls. Browser verification selected step 02 and confirmed both the active state and code-preview title changed to the matching configuration step.
- No new copy, routes, dependencies or SVG visual assets were added.
- Touched-file oxlint and formatting checks passed; the full typecheck, production build and `git diff --check` are recorded after this follow-up.

---

# Tokenflyapi 沉浸首屏与完整下半页 — 2026-09-12

final result: passed

## Current scope and visual source

- The approved immersive hero remains unchanged and still uses `output/imagegen/tokenfly-home-immersive-single-screen-v1.webp` as its source visual.
- The restored continuation uses `output/imagegen/tokenfly-home-below-cinematic-v1.webp` as the composition reference: editorial monochrome image, open model rows, dark integration interval, split FAQ and compact closing CTA.
- The implementation adapts that logic to the production project instead of reproducing the mock's invented metrics, providers or prices.
- Browser verification used the existing Codex in-app preview at `http://127.0.0.1:4174/`, Chinese, light appearance, signed out, 1019 × 832 CSS viewport.

## Current comparison and findings

1. The first viewport remains a complete background-led composition and no longer determines whether later homepage content exists.
2. The editorial ecosystem panel uses the project raster asset at its intended crop, with selectable HTML copy and three project-grounded capability lines.
3. The model section displays live configured pricing in an open, divider-led list. At the checked viewport, names, vendor, price units and model routes remain legible without card stacking.
4. The quick-start section provides the needed dark rhythm break. Its three real steps and code preview share one container; step 02 was selected successfully and updated the active state.
5. FAQ, closing CTA and project footer are restored. The closing region retains the same graphite/white system instead of introducing purple or another accent.
6. No P0/P1/P2 visual issue remained in the checked ecosystem, model, quick-start, FAQ and closing captures. Browser measurements reported `scrollWidth = 1008`, `clientWidth = 1008` and `scrollHeight = 4810`; there is no document-level horizontal overflow.

## Current engineering verification

- All newly exposed UI text reuses existing translated keys; no locale file was changed.
- The additional image uses native lazy loading and async decoding. The hero background remains the only eager image.
- Existing development-only i18next missing-key logs for runtime Chinese navigation labels remain; browser logs showed no application error or warning introduced by this work.
- Typecheck, touched-file oxlint, formatting, production build and `git diff --check` passed.

## Current follow-up boundary

- The responsive CSS includes dedicated 820px and 560px layouts. Exact physical-phone visual capture remains a P3 follow-up; no mobile P0/P1/P2 is known from the implementation and static breakpoint review.

---

# Earlier single-screen hero QA — 2026-09-12

final result: passed

## Scope and source

- Source visual truth: `/Users/apple/Documents/workplace/myproj/new-api-update/output/imagegen/tokenfly-home-immersive-single-screen-v1.webp`, 1536 × 1024 pixels.
- Generated background asset: `/Users/apple/Documents/workplace/myproj/new-api-update/web/default/src/assets/tokenfly-immersive-hero.webp`, 1536 × 1024 pixels, compressed to approximately 60KB WebP.
- Implementation: `http://127.0.0.1:4174/`, built-in homepage, Chinese, light appearance, signed out.
- Browser-rendered evidence: `/Users/apple/Documents/workplace/myproj/new-api-update/output/tokenfly-immersive-qa/implementation-desktop-final.png`.
- Browser viewport: 1020 × 832 CSS pixels, devicePixelRatio 2. The in-app browser capture is normalized to 1020 × 832 output pixels.
- The 1536 × 1024 source was scaled to 1020 × 680 and vertically letterboxed for the available in-app browser comparison; the implementation is the intentional responsive 1020 × 832 adaptation rather than a stretched source reproduction.

## Full-view and focused comparison

- Full-view same-input comparison: `/Users/apple/Documents/workplace/myproj/new-api-update/output/tokenfly-immersive-qa/comparison-final.png`.
- Focused headline, CTA, image crop and provider comparison: `/Users/apple/Documents/workplace/myproj/new-api-update/output/tokenfly-immersive-qa/comparison-heading-final.png`.
- The source and implementation are visible together in each comparison. Important typography, controls, image crop, provider index and lower proof row are legible; no additional crop was needed.

## Comparison history

1. [P2, resolved] At the initial 1020px responsive width, the robot face began behind the second headline line. The medium-width image focal point was moved right while preserving the edge-to-edge background. Post-fix evidence: `implementation-desktop-v2.png` and the final comparisons.
2. [P2, resolved] The first generated background asset was approximately 1.8MB. It was re-encoded at the original 1536 × 1024 dimensions to approximately 60KB and visually rechecked without visible degradation. Post-fix evidence: `implementation-desktop-final.png`.
3. Final comparison found no remaining actionable P0/P1/P2 issue in the implemented homepage scope.

## Required fidelity surfaces

- Fonts and typography: Inter Variable is scoped to the homepage. The live two-line headline preserves the source's oversized scale, tight tracking, strong black first line and silver-gray second line. Chinese remains selectable and translated rather than rasterized.
- Spacing and layout rhythm: the homepage is one 100svh composition with no second marketing section. Navigation, headline, CTA, provider index and proof row occupy the same background and preserve generous negative space. The available 1020px viewport correctly uses the compact navigation state with no document overflow.
- Colors and tokens: warm white, graphite, black and silver-gray match the approved monochrome target. Accent color, purple and colored gradients are absent.
- Image quality and asset fidelity: the approved robot and luminous architecture were recreated as a dedicated no-text raster background. It uses the correct right-side subject, text-safe left region and darker lower edge. No custom SVG, CSS illustration or placeholder replaces the hero image.
- Copy and content: existing translated product copy and real routes are retained. The illustrative 100+ models, 99.9% and latency claims were replaced with project-grounded 40+ channels, one API/multi-protocol compatibility and observability/usage visibility.

## Interaction and browser verification

- Primary CTA navigated to `/sign-up`; no registration was submitted.
- Provider entry navigated to `/pricing` and returned successfully.
- Pointer parallax updates CSS variables directly without React state; reduced-motion users receive a static background.
- Browser measurement: `scrollWidth = 1020`, `innerWidth = 1020`, `scrollHeight = 832`, `innerHeight = 832`.
- Browser console contained no application error or warning from this implementation. Existing development-only i18next missing-key logs for runtime Chinese navigation labels remain outside this homepage change.
- `bun run typecheck`, touched-file oxlint, touched-file formatting check, production build and `git diff --check` passed.

## Follow-up polish

- P3: verify the exact 390px mobile crop on physical mobile hardware or a user-approved emulated browser session; the responsive CSS has a dedicated text-safe overlay, focal point and compact provider/proof layout.

---

# Warm White SaaS site consistency — second milestone, 2026-09-10

Exact history restoration: read the earlier Codex task “优化首页沉浸式体验” and recovered its recorded file-change diff. Restored the original `#141612` foreground, `#cc3c10` primary, header scrim variables (`0.86`/`0.52`) and linear-gradient implementation verbatim; the scrolled header retains the recorded `0.74` glass and 24px blur. Browser computed styles confirm the exact gradient, no top-state blur and zero border. This supersedes the inference-based header iterations below.

Blur-vs-frost correction: removed the top header's 22% warm tint, saturation filter and border. It now has a fully transparent background and only `backdrop-filter: blur(18px)`, matching the supplied direct-blur reference instead of a frosted-glass treatment. Browser computed styles and screenshot confirmed transparent background, blur-only filter and zero border.

Top-header clarification: reference screenshots showed subtle glass at scroll position zero, not a fully transparent header. Restored a 22%-alpha card tint with 18px blur and 0.9 saturation; browser computed styles and screenshot confirm the artwork remains visible while navigation contrast improves. The scrolled state remains a separate 62%-alpha, 24px-blur floating capsule.

Homepage visual restoration: reverted the phase-two home-specific palette merge as one scoped change instead of incremental fixes. The homepage again owns paper/graphite/vermilion tokens, a fully transparent top-of-page header, vermilion CTA and 2px button radius. Browser computed styles confirmed transparent/no-blur at the top and 62%-alpha card background, 24px blur and 20px radius after scrolling. Console styling remains unchanged.

Glass-header regression fix: `.signal-public-header` declared an opaque glass variable directly on the header, overriding the inherited home value. Scoped the home glass variables directly to `.signal-home .brand-home-header`. Browser computed styles now confirm alpha 0.65 and `blur(24px)` both at page top and on the scrolled floating nav; scrolled screenshot inspected. CSS format check and production build passed.

Follow-up: user preferred the original colorful homepage. Removed hero grayscale/sepia and reduced-opacity overrides from `signal-home.css`; kept dark-mode brightness adjustment. Desktop browser screenshot confirmed original orange/charcoal artwork restored, with existing layout and other pages untouched. CSS formatting and production build passed. Earlier desaturation notes below describe the superseded iteration.

Result: representative visual checks passed; exhaustive business-flow regression is not claimed.

## Scope

- Shared palette, solid card borders, table surfaces, control radii and heading hierarchy; no new business behavior or copy.
- Public homepage, pricing and rankings inherit the warm-white/cocoa theme. Existing content and artwork retained, orange/green decorative palette removed or desaturated.
- Semantic status styling across keys, logs, wallet, profile, channel dialogs/status, subscriptions, system settings/info, model details, setup and playground.
- Wallet metrics reflow into one mobile column. Profile numeric typography aligned with wallet. Auth content can grow beyond viewport height.

## Browser evidence

- Local app `http://localhost:3001`, Chinese, existing administrator session, default preset.
- Desktop 1440 × 1024: wallet, usage-log table, profile, channel list, system-info tasks/instances, site settings, homepage and pricing inspected.
- Phone 390 × 844: create-key drawer (closed without saving), wallet, pricing and homepage inspected. Measured document width did not exceed viewport width on wallet, pricing or homepage. Long code examples retain local horizontal scrolling.
- Homepage client-configuration step clicked; former green integration section is now a neutral muted surface. No API request submitted.
- Site settings checked in dark mode; original system appearance restored and language left unchanged.
- Captures: `output/warm-white-site-qa/home.png`, `home-mobile.png`, `wallet-desktop.png`, `wallet-mobile.png`, `settings-dark.png`.
- Browser extension avatars and development tool badges in screenshots are not application changes.

## Limits

- No payments, key creation, configuration saves, channel tests or other mutating business actions performed.
- No exhaustive walkthrough of every modified status/dialog branch, ordinary-account session, error/loading state, locale or theme preset. Auth/setup changes inspected in code, not exercised through account/setup submission.
- Responsive viewport checks do not claim actual browser zoom-percent verification. Tables may intentionally scroll within their own containers.
- Earlier milestone records below remain historical evidence for their respective scopes.

## Code verification

- Typecheck and production build passed. Scoped lint on the 41 touched source files has no errors (three existing warnings: self-closing JSX and Number parsing style). Scoped formatting and `git diff --check` passed.
- Existing request-trend tests: `bun test src/features/dashboard/lib/overview-activity.test.ts`, 3 passed. An initial Vitest invocation used the wrong runner for this `node:test` file and reported no suite; it was rerun successfully with Bun. No project dependencies were changed.

---

# Warm White SaaS console — 2026-09-10

final result: passed

## Scope and evidence

- Scope: first implementation milestone, default palette, console shell and overview. Existing homepage review is preserved below.
- Source: `output/imagegen/warm-white-saas-recommended.png`, 1487 × 1058 pixels; scaled to 1440 × 1024 solely for comparison.
- Implementation: `http://localhost:3001/dashboard/overview`, Chinese, signed-in administrator, default palette, system/light appearance, collapsed setup guide, seven-day personal usage.
- Screenshot: `output/warm-white-qa/desktop.png`, 1440 × 1024 pixels at 1440 × 1024 CSS viewport, 1:1 capture.
- Same-input full comparison: `output/warm-white-qa/comparison.png`; focused heading/metrics comparison: `output/warm-white-qa/comparison-detail.png`.
- Additional evidence: `output/warm-white-qa/mobile.png` (390 × 844) and `output/warm-white-qa/dark.png` (1440 × 1024).

## Comparison and intentional differences

- Typography: existing sans family retained; cash/request numerals are tabular. Secondary metric text was strengthened from translucent to the semantic muted color. Existing compact navigation is denser than the illustration because all current destinations and permissions remain available.
- Layout: the mock's compact guide, shared metric surface, two-column activity/API region and recent-record table are implemented. Existing wallet/runway details and small metric trends remain. Expanded guide is available without consuming the default first screen.
- Colors: default warm white, white cards, graphite and cocoa are shared through body-level tokens, including portals. Primary white-text contrast 5.46:1; muted-on-white 5.28:1; dark primary contrast 8.44:1; dark secondary text 7.67:1.
- Assets: existing New API identity, avatar and icon library retained; no generated brand substitute, lifestyle artwork or decorative hero image added. Browser extension badges and development tools in captures are not new application features.
- Content: actual API data replaces illustrative values. Existing cumulative metrics are not mislabeled as today's totals. Recent consumption is not labeled HTTP success; configured routes remain empty if none are configured. The mock's static date and invented API metadata are intentionally absent. Additional existing admin/configured panels remain below the primary region.

## Iterations

1. Initial desktop pass exposed oversized empty supporting panels; empty states now use a compact minimum height while populated panels preserve scrolling. Post-fix evidence: desktop screenshot and comparison.
2. Dark pass exposed low-contrast Recharts default tick text; explicitly bound ticks to the muted-foreground token. Post-fix evidence: final dark screenshot, readable axis labels.
3. Final paired full-view and focused comparison: no remaining P0/P1/P2 findings within this milestone's agreed production-adaptation scope. Further density polish is P3, not a claim of pixel-identical recreation.

## Verification

- Widths checked: 390, 768, 1024, 1440, 1920 CSS pixels; document and main scroll widths do not exceed their visible widths. Chinese and English phone views checked.
- Guide expand/collapse on phone, 7/30-day selection and selected state, primary action navigation to `/keys`, theme dialog, alternate Ocean Breeze preset, and system/dark theme checked. Original language and system appearance restored.
- Typecheck, scoped lint and production build passed. Three deterministic request-trend tests passed (same-day aggregation/month boundary, range/invalid records, empty data).
- Console inspection found one development HMR reload message after locale updates; a full reload rendered successfully. No observed new application exception in the checked flows.
- Remaining coverage gaps: actual browser zoom percentages (viewport reflow was checked instead), ordinary-account UI walkthrough, simulated network-error interactions, and exhaustive regression of all existing pages. These are not represented as completed tests.

## Next milestone

Review the implemented overview, then extend structural styling to API keys, usage logs and wallet. No backend or deployment changes were made.

---

# Tokenflyapi / Signal Field design QA

final result: passed

## Scope and source

- Approved visual target: `/Users/apple/.codex/visualizations/2026/09/05/01a06f95-8050-7920-829d-4b22adcee3c1/shuaiapi-research/signal-home-desktop-v1.png` (1536 × 1024), `signal-home-mobile-v1.png` (852 × 1846), and `signal-home-continuation-v1.png` (1536 × 1024).
- Implementation: `http://localhost:3001/`, built-in homepage only, branch `codex/brand-homepage`.
- User-authorized difference: Tokenflyapi deployment brand. Existing New API identity, logo, footer, attribution, project metadata and links are retained. Production navigation, permissions and translations take precedence over illustrative mock copy/navigation.
- Intentional continuation difference: retain the working scroll-led three-step walkthrough with full runnable example instead of the mock's compressed three-line code illustration. The earlier model/language playground is preserved in an expandable section. Five existing provider examples remain instead of the mock's four; none are availability claims.

## Captures and normalization

- Evidence directory: `/Users/apple/Documents/workplace/myproj/new-api-update/output/tokenfly-qa/`.
- Desktop: `desktop-final.png`; measured CSS viewport 1536 × 1023. Browser screenshot output has an additional capture scaling factor (approximately 1.024); normalized to 1536 × 1024 for comparison.
- Mobile: `mobile-final.png`, raw 390 × 870; measured CSS viewport 389 × 844. Normalized to 390 × 845 to match the 852 × 1846 concept at 390 CSS-pixel width. Browser capture scaling and the one-pixel viewport difference are not treated as design drift.
- Tablet: measured CSS viewport 768 × 1023, no horizontal overflow, compact navigation breakpoint.
- State: Chinese, light theme, signed out, no published models; animation paused for stable hero captures. English mobile, dark desktop, resumed motion and open menu/FAQ/code states were checked separately.
- Combined full-view evidence: `compare-desktop.png` and `compare-mobile.png`, with source and implementation in one image.
- Focused evidence: `compare-desktop-detail.png` for headline/description/CTA readability; `setup-final.png` for the live dark code/step state; `models-final.png` and `faq-final.png` for supporting sections. Lower sections intentionally use the existing detailed workflow, not pixel-identical mock height.

## Comparison history and findings

1. [P2, resolved] First desktop artwork crop put dense lines behind the action copy, and the mobile portal touched the headline. Evidence: `desktop-v1.png`, `mobile-v1.png`. Moved desktop copy into a bounded right-hand column and revised artwork sizing/position at each breakpoint.
2. [P2, resolved] Intermediate contain-fit exposed the artwork's left/right edges on wide screens; mobile hero spacing delayed the following content. Changed desktop image fitting to full-bleed top alignment without stretching, and tightened mobile spacing/removing the redundant mobile scroll link. Post-fix evidence: final combined comparisons.
3. [P2, resolved] Deployment brand plus full production navigation was too dense for tablet widths. Brand-home navigation now collapses below the xl breakpoint; the ordinary public-page breakpoint remains unchanged. Checked at 768 CSS pixels without overflow.
4. [P2, resolved] Mobile had no language control and the closed navigation lacked explicit accessibility state. Added its language switcher, expanded/controls attributes, and inert/aria-hidden closed state. Checked menu open/close and English/Chinese switching.

No remaining actionable P0/P1/P2 visual findings in the tested homepage scope.

## Required fidelity surfaces

- Typography: existing bundled Public Sans with system Chinese fallback; oversized live-text wordmark, strong two-line headline, quiet body and navigation. The longer user-selected name is fitted independently on mobile. No rasterized text or lost i18n.
- Layout rhythm: full-width editorial hero, open provider/pricing rows, dark integration band, light FAQ/footer. Desktop copy and CTA remain separate from the portal. Mobile has its own vertical composition and full-width CTA. Brand preservation and richer operational content account for differences from the concept.
- Colors/tokens: paper white, graphite and vermilion; slightly darker primary than the mock for readable white button text. Dark integration band has separately scoped accessible foreground/muted/code tokens; the public dark theme remains available.
- Image quality: generated no-text filament artwork based on the selected concept; no hand-drawn substitute. 1536 × 1024 WebP at approximately 136 KiB. Original PNG retained in the QA directory, not shipped in public assets. Generated through configured host `codex.hyr.moe`.
- Copy/content: existing real translated copy retained. Provider examples explicitly qualified; real pricing hook shows its honest unconfigured state, not invented prices or operational metrics. All 63 statically detected homepage translation keys exist in all seven locales.

## Interaction / engineering verification

- Primary homepage CTA navigates to `/sign-up`; no registration submitted.
- Mobile menu opens/closes; English/Chinese switching updates the homepage.
- Mobile full-example copy reports copied success; no claim of independent clipboard readback.
- FAQ opens and displays its corresponding answer.
- Desktop step 02/03 changes highlighted configuration/request blocks on scrolling; full cURL remains intact.
- Expanded model playground updates the cURL model to `claude-sonnet-4-5` after selection; Python/cURL tabs preserved.
- Animation pauses/resumes with matching computed play state; offscreen/reduced-motion guards are implemented.
- No horizontal viewport overflow in tested mobile, tablet and desktop sizes.
- Final browser error/warning logs: empty.
- Typecheck, targeted oxlint, formatting check, production build, and `git diff --check`: passed.

## Remaining test gaps / follow-up polish

- Reduced-motion OS preference toggle was not exercised; its code paths and CSS are present. No real phone hardware test.
- Authenticated, registration-disabled, pricing-error and populated model data states were preserved by code inspection, not created by modifying the local database. Custom URL/HTML/Markdown home branches and announcements are unchanged.
- Browser language layout checked in Chinese and English; the other five locale dictionaries were statically verified.
- Development-tool badges visible at the screenshot edges are existing dev-only UI, not shipped homepage content.
- The repository contains unrelated observatory-preview and route changes; these were not changed for this homepage work.

## Implementation checklist

- [x] Preserve existing project identity and application behavior.
- [x] Implement approved artwork, editorial layout and mobile composition.
- [x] Verify translations and principal interactions.
- [x] Re-capture and compare after visual fixes.
- [x] Keep native local services and the frontend preview available; no Docker or production deployment.

---

# 控制台 / 模型广场风格统一 design QA

final result: passed

## Scope and visual truth

- Console source visual: `/Users/apple/Documents/workplace/myproj/new-api-update/output/imagegen/tokenfly-console-current-layout-home-style-v2.png` (1536 × 1024).
- Model Square source visual: `/Users/apple/Documents/workplace/myproj/new-api-update/output/imagegen/tokenfly-model-marketplace-current-layout-home-style-v2.png` (1868 × 842).
- Implemented routes: `http://localhost:3001/dashboard/overview` and `http://localhost:3001/pricing`.
- Intentional constraint: this is a style unification, not a structural rewrite. Existing data, controls, responsive breakpoints, navigation and page composition remain intact; the approved visuals supply the paper/graphite/vermilion visual language.

## Captures and comparison

- Browser-rendered implementation evidence: current Codex CUA Chrome captures in this task, not a static mock. Model Square: 1909 × 781, Chinese/light/default card-grid state. Console: 1920 × 786, Chinese/light/signed-in overview state. The screenshots were emitted in the final QA session from their respective live local routes.
- Full-view comparison: both source images and live captures were opened for this QA pass. The implementation intentionally preserves the production pages' denser navigation and operational content, while matching the target treatment in the visible page frame, warm surfaces, graphite type, vermilion emphasis, borders, shadow restraint and card rounding.
- Focused comparison: Model Square search, filter rail, price toolbar, cards and detail drawer; Console sidebar, setup guide, request preview, quick actions, usage cards and balance panel. Additional crop-level comparison was unnecessary because these regions are legible in the live desktop captures.
- Density normalization: not applied. The two approved concepts and the live browser windows have different heights because the pages retain their current layouts; the style review compares the shared visible regions rather than treating that intentional content-depth difference as a mismatch.

## Findings

No actionable P0/P1/P2 findings remain.

- Fonts and typography: the existing Public Sans body text is retained for functional UI, with the Model Square display heading using the homepage's editorial serif treatment. Existing translated and live model data remain selectable/readable.
- Spacing and layout rhythm: the original grids and responsive structure are intact. Scoped spacing, one-pixel borders, restrained elevation and a 6–12px corner-radius system keep the views cohesive without flattening the existing functional density.
- Colors and tokens: both routes use the scoped warm paper, graphite, muted stone and vermilion token set. Selected filters, primary actions and active navigation have the same vermilion hierarchy as the approved homepage direction; dark-mode tokens remain defined.
- Image quality and assets: the approved homepage ribbon asset at `/Users/apple/Documents/workplace/myproj/new-api-update/web/default/public/brand/signal-ribbon-cascade-v2.jpg` is reused as the shared art direction. No new decorative asset was needed, and no target imagery was replaced with CSS or handcrafted SVG art.
- Copy and content: real production copy, model names, prices, permissions and translated text are preserved rather than replaced with visual-only mock content.
- Accessibility and responsive behavior: controls retain their existing semantic elements, visible focus styling and labels. The final responsive pass preserved the mobile search/filter/card/detail-drawer flow; no horizontal overflow was observed in the earlier mobile verification.

## Interaction and engineering verification

- Model Square search, clear-search, filters, grid/table control and model-detail drawer remain functional; search/clear and detail opening were exercised in the local preview.
- Console navigation, onboarding links and primary CTAs retain their original routes/behavior; no API-key creation or other stateful action was submitted.
- Browser error/warning log was empty during the final visual verification.
- `bun run typecheck`, touched-file `oxlint`, touched-file formatting check, `bun run build`, and `git diff --check` passed.
- The repository-wide `bun run format:check` still reports pre-existing formatting issues in unrelated files; no formatter failures are present in the files touched for this UI scope.

## Implementation checklist

- [x] Add scoped homepage-style tokens without changing global application behavior.
- [x] Apply the style system to the existing Console and Model Square surfaces.
- [x] Preserve round cards, controls and drawers across desktop and mobile.
- [x] Verify the live browser result against both approved visuals.
- [x] Keep the local preview available; no deployment was performed.

---

# 公共顶栏一致性 design QA

final result: passed

## Scope and root cause

- Verified transition: homepage `/` → Model Square `/pricing` through the real top navigation.
- Root cause: the homepage supplied a hardcoded Tokenflyapi brand and homepage-only header geometry, while other public routes used runtime `New API` branding and separate pricing overrides.
- Fix: `PublicLayout` now provides the shared Tokenflyapi mark/name and `signal-public-header` class to every public route, while preserving explicit caller overrides.

## Browser verification

- Browser: the user's own Google Chrome session, tab `225395138`, approximately 1908 × 837 CSS pixels.
- Unscrolled state: homepage → Model Square client-side navigation keeps the same brand, horizontal alignment, container width and navigation position.
- Scrolled state: Model Square retains the homepage-style 60px rounded glass header; corners, border and shadow remain visible and balanced at wide-screen size.
- Existing page content, filters, model cards and navigation behavior remain unchanged.
- Browser logs contain no error or warning introduced by this change; existing development-only i18next missing-key logs are unrelated to the header patch.

## Engineering verification

- Targeted `oxfmt --check`: passed.
- Targeted `oxlint`: passed.
- `bun run typecheck`: passed.
- `bun run build`: passed.
- `git diff --check`: passed.
