from playwright.sync_api import sync_playwright
import json, os
from pathlib import Path
BASE=os.environ.get("SCHEDULING_BROWSER_URL","http://127.0.0.1:58080")
OUTPUT=Path(os.environ.get("SCHEDULING_BROWSER_OUTPUT","/tmp/herbstfest-browser"))
OUTPUT.mkdir(parents=True,exist_ok=True)
with sync_playwright() as p:
 b=p.chromium.launch(headless=True)
 page=b.new_page(viewport={'width':1440,'height':1000})
 errors=[];page.on('pageerror',lambda e:errors.append(str(e)))
 page.goto(BASE+'/',wait_until='networkidle')
 assert page.locator('.sidebar').count()==1
 assert page.locator('.topbar').count()==1
 assert page.locator('.schedule-section').count()==10
 assert page.locator('.timeline article').count()==150
 page.screenshot(path=str(OUTPUT/'organizer.png'),full_page=False)
 page.goto(BASE+'/print/stand-00')
 assert 'Druckstand:' in page.inner_text('body')
 assert page.locator('form').count()==0
 page.emulate_media(media='print');page.pdf(path=str(OUTPUT/'roster.pdf'))
 page=b.new_page(viewport={'width':390,'height':844})
 page.on('pageerror',lambda e:errors.append(str(e)))
 page.goto(BASE+'/register/synthetic-rehearsal-registration-only')
 assert page.locator('input[name=email]').count()==0
 assert 'Testperson' not in page.inner_text('body')
 page.locator('input[name=first_name]').fill('Mobil')
 page.locator('input[name=last_name]').fill('Testgast')
 page.locator('input[name=shift]').first.check()
 assert page.evaluate('document.documentElement.scrollWidth<=innerWidth')
 page.screenshot(path=str(OUTPUT/'phone.png'),full_page=False)
 page.get_by_role('button',name='Alle ausgewählten Dienste anfragen').click()
 page.get_by_role('heading',name='Deine Anfragen sind gespeichert.').wait_for()
 assert page.get_by_role('link',name='Anmelden oder Konto erstellen').count()==1
 page.goto(BASE+'/design-system')
 assert page.get_by_role('link',name='Helfer suchen').count()==0
 assert page.get_by_role('link',name='Organisation',exact=True).count()==0
 print(json.dumps({'desktop_stands':10,'desktop_shifts':150,'phone_signup':'passed','phone_overflow':False,'print_pdf':'passed','public_navigation':'passed','javascript_errors':errors}))
 assert not errors
 b.close()
