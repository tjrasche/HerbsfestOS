"""Exercise the real server-rendered UI against disposable local fixtures.

Run with Playwright Chromium installed. Never target a live application: these
checks may submit synthetic forms. The default fixture is the local rehearsal.
Optional EMPTY/STAND/MEMBER URLs enable additional trusted-DB role fixtures.
"""
import asyncio
from contextlib import suppress
import json
import os
from pathlib import Path
from urllib.parse import parse_qs, quote, urlparse
from uuid import uuid4

from playwright.async_api import Error as PlaywrightError, async_playwright


BASE = os.getenv("SCHEDULING_BROWSER_URL", "http://127.0.0.1:58081").rstrip("/")
OUTPUT = Path(os.getenv("SCHEDULING_BROWSER_OUTPUT", "/tmp/herbstfest-ui-redesign"))
OUTPUT.mkdir(parents=True, exist_ok=True)
ROLE_URLS = {
    role: os.getenv(f"SCHEDULING_BROWSER_{role.upper()}_URL", "").rstrip("/")
    for role in ("empty", "stand", "member", "outage")
}
for target in (BASE, *filter(None, ROLE_URLS.values())):
    parsed = urlparse(target)
    if parsed.hostname not in ("127.0.0.1", "localhost") or parsed.scheme != "http":
        raise SystemExit("Browser mutation checks require disposable loopback services")


async def assert_fits(page):
    assert await page.evaluate("document.documentElement.scrollWidth <= innerWidth"), (
        f"document overflows at {page.viewport_size}: {page.url}"
    )


async def assert_frame(page):
    # A history miss deliberately reloads for fresh authorization. popstate can
    # return before that new document has parsed, so wait for the actual frame.
    await page.locator("#app-frame").wait_for(state="visible")
    assert await page.locator("#app-frame").count() == 1, page.url
    assert await page.locator("#main").count() == 1, page.url
    assert await page.locator("html").count() == 1, page.url
    assert await page.locator("h1").count() >= 1, page.url


async def mutation_finished(page, response):
    await page.wait_for_function("document.getElementById('app-frame')?.getAttribute('aria-busy') !== 'true'")
    destination = response.headers.get("hx-replace-url")
    if destination:
        await page.wait_for_url("**" + destination)
    await assert_frame(page)


async def check_search_and_network(page):
    await page.goto(BASE + "/directory", wait_until="networkidle")
    query = page.locator('input[name="q"]')
    assert await page.locator("[data-worker-id]").count() == 0
    await query.fill("Beispiel")
    await page.wait_for_url("**/directory?**q=Beispiel**")
    await page.locator("[data-worker-id]").first.wait_for()
    assert await page.locator("[data-worker-id]").count() == 25
    assert await page.locator('input[name="q"]').evaluate("element => element === document.activeElement")
    first_ids = await page.locator("[data-worker-id]").evaluate_all("nodes => nodes.map(node => node.dataset.workerId)")
    next_page = page.locator('a[rel="next"]').first
    if not await next_page.count():
        next_page = page.get_by_role("link", name="Weiter", exact=True)
    await next_page.click()
    await page.wait_for_url("**page=2**")
    await page.wait_for_function("document.getElementById('app-frame')?.getAttribute('aria-busy') !== 'true'")
    second_ids = await page.locator("[data-worker-id]").evaluate_all("nodes => nodes.map(node => node.dataset.workerId)")
    assert 0 < len(second_ids) <= 25 and not set(first_ids).intersection(second_ids)

    await page.goto(BASE + "/directory", wait_until="networkidle")
    started, release, finished = asyncio.Event(), asyncio.Event(), asyncio.Event()
    await page.evaluate("window.__abortedSearches = 0; document.addEventListener('htmx:sendAbort', () => window.__abortedSearches++)")
    async def hold_older_search(route):
        if "q=ZZ" in route.request.url:
            started.set()
            await release.wait()
            # The older XHR is deliberately canceled by htmx before release.
            with suppress(PlaywrightError):
                await route.continue_()
            finished.set()
        else:
            await route.continue_()
    await page.route("**/directory?**", hold_older_search)
    try:
        await page.locator('input[name="q"]').fill("ZZ")
        await asyncio.wait_for(started.wait(), timeout=10)
        await page.locator('input[name="q"]').fill("Beispiel")
        await page.wait_for_url("**/directory?**q=Beispiel**")
        await page.locator("[data-worker-id]").first.wait_for()
        assert await page.evaluate("window.__abortedSearches") > 0
    finally:
        release.set()
        await asyncio.wait_for(finished.wait(), timeout=10)
        await page.unroute("**/directory?**", hold_older_search)
    await page.wait_for_function("document.getElementById('app-frame')?.getAttribute('aria-busy') === 'false'")
    assert await page.locator('input[name="q"]').input_value() == "Beispiel"
    assert await page.locator("[data-worker-id]").count() == 25

    # Abort requests in the browser before they reach the disposable service.
    # Existing input must survive and pending controls must be usable afterward.
    await page.goto(BASE + "/admin/editions", wait_until="networkidle")
    async def fail_network(route):
        await route.abort("failed")
    await page.route("**/admin/editions/new*", fail_network)
    await page.locator('a[href^="/admin/editions/new"]').first.click()
    await page.locator("#network-error").wait_for(state="visible")
    await page.wait_for_function("document.getElementById('app-frame')?.getAttribute('aria-busy') === 'false'")
    assert not await page.locator("#app-frame").evaluate("element => element.inert")
    await page.unroute("**/admin/editions/new*", fail_network)
    await page.goto(BASE + "/admin/editions/new", wait_until="networkidle")
    await page.locator('input[name="label"]').fill("Unsaved network check")
    await page.locator('input[name="start_day"]').fill("2028-09-10")
    await page.locator('input[name="end_day"]').fill("2028-09-12")
    await page.route("**/admin/editions*", fail_network)
    form = page.locator('form').filter(has=page.locator('input[name="label"]'))
    await form.locator('button[type="submit"]').click()
    await page.locator("#network-error").wait_for(state="visible")
    await page.wait_for_function("document.getElementById('app-frame')?.getAttribute('aria-busy') === 'false'")
    assert "bereits gespeichert" in await page.locator("#network-error").inner_text()
    assert await page.locator('input[name="label"]').input_value() == "Unsaved network check"
    assert await form.locator('button[type="submit"]').is_enabled()
    await page.screenshot(path=str(OUTPUT / "network-mutation.png"))
    await page.unroute("**/admin/editions*", fail_network)


async def check_focused_shift(page):
    await page.goto(BASE + "/", wait_until="networkidle")
    detail = page.locator('a[href^="/shifts/"]').first
    detail_path = urlparse(await detail.get_attribute("href")).path
    await detail.click()
    await page.wait_for_url("**" + detail_path + "**")
    await page.get_by_role("link", name="Dienst bearbeiten", exact=True).click()
    await page.locator('input[name="title"]').fill("Festdienst UI geprüft")
    # Three confirmed fixture assignments must block a reduction to two places.
    await page.locator('input[name="capacity"]').fill("2")
    async with page.expect_response(lambda response: response.request.method == "POST") as conflict:
        await page.get_by_role("button", name="Dienst speichern", exact=True).click()
    response = await conflict.value
    assert response.status == 409
    await mutation_finished(page, response)
    assert await page.locator('input[name="title"]').input_value() == "Festdienst UI geprüft"
    assert await page.locator('input[name="capacity"]').input_value() == "2"
    assert urlparse(page.url).path == detail_path + "/edit"
    await page.locator('input[name="capacity"]').fill("5")
    async with page.expect_response(lambda response: response.request.method == "POST") as saved:
        await page.get_by_role("button", name="Dienst speichern", exact=True).click()
    assert (await saved.value).status == 200
    await mutation_finished(page, await saved.value)
    assert urlparse(page.url).path == detail_path
    assert await page.locator("h1").inner_text() == "Festdienst UI geprüft"
    assert "gespeichert" in (await page.locator("#main").inner_text()).lower()
    assert len(await page.content()) <= 100 * 1024
    await page.screenshot(path=str(OUTPUT / "shift-detail.png"))

    # Issue only a synthetic local signup link and check one-time presentation.
    link_form = page.locator(f'form[action^="{detail_path}/links"]')
    await link_form.locator('input[name="confirm"]').check()
    async with page.expect_response(lambda response: response.request.method == "POST") as issued:
        await link_form.locator('button[type="submit"]').click()
    assert (await issued.value).status == 200
    await mutation_finished(page, await issued.value)
    copied = await page.locator('[data-copy-target]').get_attribute("data-copy-target")
    secret_input = page.locator('[id="' + copied + '"]')
    token_url = await secret_input.input_value()
    assert token_url.startswith(BASE + "/register/")
    await page.get_by_role("link", name="Linkstatus verwalten", exact=True).click()
    await page.wait_for_url("**/links/**")
    assert await page.locator('[data-copy-target]').count() == 0
    assert token_url not in await page.content(), "management recovered a bearer URL"


async def check_detail_scroll(page):
    for width in (1440, 390):
        await page.set_viewport_size({"width": width, "height": 1000 if width == 1440 else 844})
        await page.goto(BASE + "/directory?q=Beispiel", wait_until="networkidle")
        last_profile = page.locator('[data-worker-id]').last.get_by_role("link", name="Angaben öffnen", exact=True)
        await last_profile.scroll_into_view_if_needed()
        assert await page.evaluate("scrollY") > 500
        await last_profile.click()
        await page.wait_for_url("**/workers/**")
        await page.wait_for_function("document.getElementById('app-frame')?.getAttribute('aria-busy') !== 'true'")
        heading = await page.locator("h1").bounding_box()
        assert heading and 0 <= heading["y"] < page.viewport_size["height"] - 60, (
            f"detail heading is outside the viewport after navigation at {width}px: {heading}"
        )
        await assert_fits(page)
    await page.set_viewport_size({"width": 1440, "height": 1000})


async def check_membership_context(page):
    # Create a disposable existing helper at another stand, then add the same
    # record through the real originating-stand directory workflow.
    name = "Browsermitglied " + uuid4().hex[:10]
    await page.goto(BASE + "/stands/stand-01/members", wait_until="networkidle")
    await page.locator('input[name="first_name"]').fill(name)
    await page.locator('input[name="last_name"]').fill("UI Prüfung")
    form = page.locator('form').filter(has=page.locator('input[name="first_name"]'))
    async with page.expect_response(lambda response: response.request.method == "POST") as created:
        await form.locator('button[type="submit"]').click()
    assert (await created.value).status == 200
    await mutation_finished(page, await created.value)
    await page.goto(BASE + "/stands/stand-00/members", wait_until="networkidle")
    await page.get_by_role("link", name="Im Helferverzeichnis suchen", exact=True).click()
    await page.wait_for_url("**/directory?**")
    await page.locator("#directory-q").wait_for(state="visible")
    await page.wait_for_function("document.getElementById('app-frame')?.getAttribute('aria-busy') !== 'true'")
    assert parse_qs(urlparse(page.url).query).get("member_stand") == ["stand-00"]
    await page.locator("#directory-q").fill(name)
    await page.locator('[data-worker-id]').first.wait_for()
    row = page.locator('[data-worker-id]').filter(has_text=name)
    add = row.locator('form[action^="/stands/stand-00/members"]')
    assert await add.count() == 1, "search offers no direct membership action for the originating stand"
    assert "hinzufügen" in (await add.locator('button[type="submit"]').inner_text()).lower()
    async with page.expect_response(lambda response: response.request.method == "POST") as joined:
        await add.locator('button[type="submit"]').click()
    assert (await joined.value).status == 200
    await mutation_finished(page, await joined.value)
    assert urlparse(page.url).path == "/directory"
    assert parse_qs(urlparse(page.url).query).get("member_stand") == ["stand-00"]
    assert "Bereits beim Stand" in await page.locator('[data-worker-id]').filter(has_text=name).inner_text()
    await page.goto(BASE + "/stands/stand-00/members?q=" + quote(name), wait_until="networkidle")
    assert name in await page.locator("#main").inner_text()


async def check_interactive_navigation(page):
    """Hold an actual htmx request to inspect its in-flight UI deterministically."""
    await page.goto(BASE + "/admin/editions", wait_until="networkidle")
    await page.evaluate("window.__redesignDocumentMarker = 'same-document'")
    started, release, finished = asyncio.Event(), asyncio.Event(), asyncio.Event()
    requests = []

    async def hold(route):
        requests.append(route.request)
        started.set()
        await release.wait()
        await route.continue_()
        finished.set()

    await page.route("**/admin/editions/new*", hold)
    click = asyncio.create_task(page.locator('a[href^="/admin/editions/new"]').first.click())
    try:
        await asyncio.wait_for(started.wait(), timeout=10)
        assert requests[0].headers.get("hx-request") == "true", "navigation reloaded the document"
        await page.wait_for_function("document.getElementById('page-loading')?.classList.contains('htmx-request')")
        await page.wait_for_function("""() => [...document.querySelectorAll('[data-skeleton], [class*="skeleton"]')].some(node => {
          const box = node.getBoundingClientRect();
          if (!box.width || !box.height) return false;
          for (let parent = node; parent; parent = parent.parentElement) {
            const style = getComputedStyle(parent);
            if (style.display === 'none' || style.visibility === 'hidden' || Number(style.opacity) < 0.05) return false;
          }
          return true;
        })""")
        assert await page.locator("#app-frame").get_attribute("aria-busy") == "true"
        await page.screenshot(path=str(OUTPUT / "navigation-loading.png"))
    finally:
        release.set()
        await click
        await asyncio.wait_for(finished.wait(), timeout=10)
        await page.unroute("**/admin/editions/new*", hold)
    await page.wait_for_url("**/admin/editions/new*")
    await page.wait_for_load_state("networkidle")
    await assert_frame(page)
    assert await page.evaluate("window.__redesignDocumentMarker") == "same-document"
    await page.wait_for_function("!document.getElementById('page-loading')?.classList.contains('htmx-request')")
    # Protected pages must not leave an htmx HTML history snapshot on the client.
    for storage in ("localStorage", "sessionStorage"):
        assert await page.evaluate(f"{storage}.getItem('htmx-history-cache')") in (None, "[]")
    await page.go_back(wait_until="networkidle")
    await assert_frame(page)
    assert urlparse(page.url).path == "/admin/editions"
    await page.go_forward(wait_until="networkidle")
    await assert_frame(page)
    assert urlparse(page.url).path == "/admin/editions/new"


async def check_first_edition(page, target):
    await page.goto(target + "/admin", wait_until="networkidle")
    create = page.get_by_role("link", name="Erste Ausgabe erstellen")
    assert await create.count() == 1, "empty global setup has no first-edition action"
    await page.screenshot(path=str(OUTPUT / "first-edition-empty.png"))
    await create.click()
    await page.locator('input[name="label"]').fill("UI Erstausgabe")
    await page.locator('input[name="start_day"]').fill("2028-09-10")
    await page.locator('input[name="end_day"]').fill("2028-09-12")
    async with page.expect_response(lambda response: response.request.method == "POST") as created:
        await page.locator('form').filter(has=page.locator('input[name="label"]')).locator('button[type="submit"]').click()
    response = await created.value
    assert response.status == 200
    await mutation_finished(page, response)
    edition_path = urlparse(page.url).path
    assert edition_path.startswith("/admin/editions/") and not edition_path.endswith("/new")
    assert "UI Erstausgabe" in await page.locator("#main").inner_text()
    await page.goto(target + "/admin/stands/new", wait_until="networkidle")
    await page.locator('input[name="name"]').fill("UI Teststand")
    async with page.expect_response(lambda response: response.request.method == "POST") as saved_stand:
        await page.locator('form').filter(has=page.locator('input[name="name"]')).locator('button[type="submit"]').click()
    await mutation_finished(page, await saved_stand.value)
    await page.goto(target + edition_path, wait_until="networkidle")
    await page.locator('select[name="stand_id"]').select_option(label="UI Teststand")
    async with page.expect_response(lambda response: response.request.method == "POST") as participation:
        await page.locator('form').filter(has=page.locator('select[name="stand_id"]')).locator('button[type="submit"]').click()
    await mutation_finished(page, await participation.value)
    assert "Team fehlt" in await page.locator("#main").inner_text()
    await page.locator(f'a[href^="{edition_path}/participations/"]').first.click()
    async with page.expect_response(lambda response: response.request.method == "POST") as team:
        await page.get_by_role("button", name="Mich zum Standteam hinzufügen", exact=True).click()
    await mutation_finished(page, await team.value)
    await page.goto(target + edition_path, wait_until="networkidle")
    activation = page.locator(f'form[action^="{edition_path}/activate"]')
    await activation.locator('input[name="confirm"]').check()
    async with page.expect_response(lambda response: response.request.method == "POST") as activated:
        await activation.locator('button[type="submit"]').click()
    assert (await activated.value).status == 200
    await mutation_finished(page, await activated.value)
    assert "Aktuelle Ausgabe" in await page.locator("#main").inner_text()
    await page.screenshot(path=str(OUTPUT / "first-edition-activated.png"))


async def run():
    results = {}
    javascript_errors = []
    async with async_playwright() as playwright:
        browser = await playwright.chromium.launch(headless=True)
        context = await browser.new_context(viewport={"width": 1440, "height": 1000})
        page = await context.new_page()
        page.on("pageerror", lambda error: javascript_errors.append(str(error)))

        response = await page.goto(BASE + "/", wait_until="networkidle")
        assert response.status == 200
        await assert_frame(page)
        form_count = await page.locator("form").count()
        assert form_count <= 8, f"summary rendered {form_count} forms"
        html_bytes = len((await response.body()))
        assert html_bytes <= 180 * 1024, f"summary rendered {html_bytes} bytes"
        await page.screenshot(path=str(OUTPUT / "schedule-desktop.png"))
        results["summary"] = {"forms": form_count, "html_bytes": html_bytes}

        destinations = (
            "/admin", "/admin/editions", "/admin/editions/new", "/admin/stands",
            "/admin/access", "/admin/invitations", "/directory?q=Beispiel",
            "/requests/mine", "/requests/pending", "/feedback-notes",
        )
        for path in destinations:
            response = await page.goto(BASE + path, wait_until="networkidle")
            assert response.status == 200, (path, response.status)
            await assert_frame(page)
            assert "no-store" in response.headers.get("cache-control", ""), path
        results["direct_pages"] = len(destinations)

        await check_interactive_navigation(page)
        results["htmx_navigation_loading_history"] = True

        await page.goto(BASE + "/admin/editions/new", wait_until="networkidle")
        await page.locator('input[name="label"]').fill("Browser validation check")
        await page.locator('input[name="start_day"]').fill("2028-09-12")
        await page.locator('input[name="end_day"]').fill("2028-09-10")
        edition_form = page.locator('form').filter(has=page.locator('input[name="label"]'))
        submit = edition_form.locator('button[type="submit"]').first
        started, release, finished = asyncio.Event(), asyncio.Event(), asyncio.Event()

        async def hold_mutation(route):
            if route.request.method == "POST":
                started.set()
                await release.wait()
            await route.continue_()
            finished.set()

        await page.route("**/admin/editions*", hold_mutation)
        async with page.expect_response(lambda response: response.request.method == "POST") as pending:
            click = asyncio.create_task(submit.click())
            try:
                await asyncio.wait_for(started.wait(), timeout=10)
                assert await submit.is_disabled(), "pending mutation is still clickable"
                await page.screenshot(path=str(OUTPUT / "form-pending.png"))
            finally:
                release.set()
                await click
                await asyncio.wait_for(finished.wait(), timeout=10)
        await page.unroute("**/admin/editions*", hold_mutation)
        response = await pending.value
        assert response.status == 422, response.status
        await mutation_finished(page, response)
        await assert_frame(page)
        assert await page.locator('input[name="label"]').input_value() == "Browser validation check"
        assert await page.locator('input[name="start_day"]').input_value() == "2028-09-12"
        assert await page.locator('input[name="end_day"]').input_value() == "2028-09-10"
        assert await page.get_by_role("alert").count() > 0
        assert await page.locator('form').filter(has=page.locator('input[name="label"]')).locator('button[type="submit"]').first.is_enabled()
        await page.screenshot(path=str(OUTPUT / "validation.png"))
        results["validation_preserves_form"] = True

        await check_search_and_network(page)
        results["directory_debounced_search_and_paging"] = True
        results["network_cleanup_and_uncertain_mutation"] = True

        await check_focused_shift(page)
        results["capacity_conflict_and_contextual_shift_save"] = True
        results["absolute_one_time_link_and_safe_management"] = True

        await check_detail_scroll(page)
        results["detail_navigation_reveals_heading"] = True
        await check_membership_context(page)
        results["existing_worker_membership_retains_stand"] = True

        for width in (320, 390, 768):
            await page.set_viewport_size({"width": width, "height": 844})
            for path in ("/", "/admin", "/admin/editions/new", "/directory?q=Beispiel", "/feedback-notes"):
                await page.goto(BASE + path, wait_until="networkidle")
                await assert_fits(page)
            if width == 390:
                await page.goto(BASE + "/admin", wait_until="networkidle")
                await page.screenshot(path=str(OUTPUT / "admin-phone.png"))
        results["responsive_widths"] = [320, 390, 768]

        await page.set_viewport_size({"width": 390, "height": 844})
        response = await page.goto(BASE + "/register/synthetic-rehearsal-registration-only", wait_until="networkidle")
        assert response.status == 200
        await assert_fits(page)
        assert await page.locator('input[name="email"],input[name="phone"]').count() == 0
        assert await page.locator('a[href="/admin"]').count() == 0
        await page.locator('input[name="first_name"]').fill("UI Browser")
        await page.locator('input[name="last_name"]').fill("Prüfung")
        guest_form = page.locator('form').filter(has=page.locator('input[name="first_name"]'))
        async with page.expect_response(lambda response: response.request.method == "POST") as empty_selection:
            await guest_form.locator('button[type="submit"]').click()
        assert (await empty_selection.value).status == 422
        await mutation_finished(page, await empty_selection.value)
        assert await page.locator('input[name="first_name"]').input_value() == "UI Browser"
        choices = page.locator('input[type="checkbox"][name="shift"]:enabled')
        assert await choices.count() >= 2
        await choices.nth(0).check()
        await choices.nth(1).check()
        await page.screenshot(path=str(OUTPUT / "signup-phone.png"))
        async with page.expect_response(lambda response: response.request.method == "POST") as submitted:
            await page.locator('form').filter(has=page.locator('input[name="first_name"]')).locator('button[type="submit"]').click()
        assert (await submitted.value).status == 200
        await mutation_finished(page, await submitted.value)
        text = await page.locator("#main").inner_text()
        assert "UI Browser" in text and "Prüfung" in text, "receipt omits submitted names"
        assert "Festdienst" in text, "receipt omits the selected shifts"
        assert "Bestätigung" in text or "offen" in text.lower(), "receipt omits pending approval"
        await page.screenshot(path=str(OUTPUT / "signup-receipt.png"))
        results["names_only_guest_form"] = True
        results["guest_validation_and_receipt"] = True

        await page.set_viewport_size({"width": 1440, "height": 1000})
        response = await page.goto(BASE + "/print/stand-00", wait_until="networkidle")
        assert response.status == 200
        assert await page.locator("form").count() == 0
        await page.emulate_media(media="print")
        await page.pdf(path=str(OUTPUT / "roster.pdf"), format="A4")
        await page.emulate_media(media="screen")
        results["print_pdf"] = True

        await page.goto(BASE + "/design-system", wait_until="networkidle")
        await page.get_by_role("heading", name="Ein Fest. Ein gemeinsamer Look.", exact=True).wait_for()
        assert await page.get_by_role("link", name="Helfer suchen", exact=True).count() == 0
        assert await page.get_by_role("link", name="Organisation", exact=True).count() == 0
        results["public_reference_navigation"] = True

        for role, target in ROLE_URLS.items():
            if not target:
                results[role + "_fixture"] = "not configured"
                continue
            role_page = await context.new_page()
            role_page.on("pageerror", lambda error: javascript_errors.append(str(error)))
            response = await role_page.goto(target + "/", wait_until="networkidle")
            if role == "outage":
                assert response.status == 503
                await assert_frame(role_page)
                assert "Testperson" not in await role_page.locator("#main").inner_text()
                assert await role_page.locator('a[href="/admin"]').count() == 0
                results[role + "_fixture"] = "passed"
                await role_page.close()
                continue
            assert response.status == 200, (role, response.status)
            await assert_frame(role_page)
            if role in ("stand", "member"):
                response = await role_page.goto(target + "/admin", wait_until="networkidle")
                assert response.status == 403, (role, response.status)
                await assert_frame(role_page)
            if role == "member":
                response = await role_page.goto(target + "/directory?q=Beispiel", wait_until="networkidle")
                assert response.status == 403
                response = await role_page.goto(target + "/requests/mine", wait_until="networkidle")
                assert response.status == 200
            if role == "empty":
                await check_first_edition(role_page, target)
            await role_page.screenshot(path=str(OUTPUT / (role + "-fixture.png")))
            results[role + "_fixture"] = "passed"
            await role_page.close()

        no_js = await browser.new_context(java_script_enabled=False)
        no_js_page = await no_js.new_page()
        await no_js_page.goto(BASE + "/admin/editions/new")
        await no_js_page.locator('input[name="label"]').fill("No JavaScript validation")
        await no_js_page.locator('input[name="start_day"]').fill("2028-09-12")
        await no_js_page.locator('input[name="end_day"]').fill("2028-09-10")
        async with no_js_page.expect_response(lambda response: response.request.method == "POST") as no_js_result:
            await no_js_page.locator('form').filter(has=no_js_page.locator('input[name="label"]')).locator('button[type="submit"]').click()
        assert (await no_js_result.value).status == 422
        await assert_frame(no_js_page)
        assert await no_js_page.locator('input[name="label"]').input_value() == "No JavaScript validation"
        await no_js.close()
        results["no_javascript_form_fallback"] = True

        assert not javascript_errors, javascript_errors
        results["javascript_errors"] = javascript_errors
        await browser.close()
    print(json.dumps(results, ensure_ascii=False))


if __name__ == "__main__":
    asyncio.run(run())
