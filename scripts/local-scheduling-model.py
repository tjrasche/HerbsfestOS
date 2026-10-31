#!/usr/bin/env python3
"""Create an empty contextual-only store on a disposable loopback OpenFGA."""
import json, sys, urllib.request, urllib.parse
from pathlib import Path
base=sys.argv[1] if len(sys.argv)>1 else 'http://127.0.0.1:58180'
u=urllib.parse.urlparse(base)
if u.scheme!='http' or u.hostname!='127.0.0.1':raise SystemExit('Only disposable loopback OpenFGA is supported')
def post(path, data):
    r=urllib.request.Request(base+path,data=json.dumps(data).encode(),headers={'Content-Type':'application/json'})
    with urllib.request.urlopen(r,timeout=10) as response:return json.load(response)
store=post('/stores',{'name':'herbstfest-local-scheduling'})['id']
model=post(f'/stores/{store}/authorization-models',json.loads((Path(__file__).resolve().parents[1]/'internal/scheduling/authorization-model.json').read_text()))['authorization_model_id']
print(f'export OPENFGA_API_URL={base}\nexport OPENFGA_STORE_ID={store}\nexport OPENFGA_AUTHORIZATION_MODEL_ID={model}')
