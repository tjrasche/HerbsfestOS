#!/usr/bin/env python3
"""Check the proposed model against a disposable, in-memory OpenFGA server.
Usage: python3 scripts/validate-scheduling-authorization.py --binary /path/to/openfga
Never uses configured application services, credentials, or persisted stores.
"""
import argparse, json, socket, subprocess, tempfile, time, urllib.request
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
def port():
    with socket.socket() as s:
        s.bind(('127.0.0.1',0)); return s.getsockname()[1]
def tuple_(user, relation, obj): return dict(user=user,relation=relation,object=obj)
def run(binary):
    http_port,grpc_port=port(),port()
    base=f'http://127.0.0.1:{http_port}'
    with tempfile.TemporaryFile() as logs:
        server=subprocess.Popen([binary,'run','--datastore-engine=memory',f'--http-addr=127.0.0.1:{http_port}',f'--grpc-addr=127.0.0.1:{grpc_port}','--playground-enabled=false','--metrics-enabled=false'],stdout=logs,stderr=logs)
        try:
            def post(path,data):
                req=urllib.request.Request(base+path,data=json.dumps(data).encode(),headers={'Content-Type':'application/json'})
                with urllib.request.urlopen(req,timeout=5) as r:return json.load(r)
            for _ in range(100):
                if server.poll() is not None:
                    logs.seek(0);raise RuntimeError(logs.read().decode())
                try:
                    with urllib.request.urlopen(base+'/healthz',timeout=.2):break
                except OSError:time.sleep(.1)
            else:raise RuntimeError('OpenFGA did not become ready')
            store=post('/stores',{'name':'wayfinder-disposable-validation'})['id']
            model=post(f'/stores/{store}/authorization-models',json.loads((ROOT/'docs/wayfinder/volunteer-scheduling/validation/authorization-model.json').read_text()))['authorization_model_id']
            prefix=f'/stores/{store}'
            basefacts=[tuple_('festival:herbstfest','festival','stand:beer'),tuple_('participation:beer-2027','participation','stand:beer'),tuple_('festival:herbstfest','festival','participation:beer-2027'),tuple_('stand:beer','stand','participation:beer-2027'),tuple_('participation:beer-2027','participation','shift:setup'),tuple_('festival:herbstfest','festival','worker:anna'),tuple_('stand:beer','member_stand','worker:anna'),tuple_('festival:herbstfest','festival','request:one'),tuple_('worker:anna','worker','request:one'),tuple_('shift:setup','shift','request:one')]
            facts={
              'global':tuple_('principal:alice','global','festival:herbstfest'),
              'team':tuple_('principal:alice','team','participation:beer-2027'),
              'member':tuple_('principal:alice','member','stand:beer'),
              'current':tuple_('principal:*','current','participation:beer-2027'),
              'enabled':tuple_('principal:*','enabled','participation:beer-2027'),
              'owner':tuple_('principal:alice','owner','worker:anna'),
              'signup':tuple_('principal:alice','signup','shift:setup')}
            cases=[
             ('global manages catalog',['global'],'festival:herbstfest','manage_catalog',True),
             ('member cannot administer grants',['member','current','enabled'],'festival:herbstfest','manage_global_grants',False),
             ('global creates feedback',['global'],'festival:herbstfest','create_feedback',True),
             ('stand organizer cannot view feedback',['team','current','enabled'],'festival:herbstfest','view_feedback',False),
             ('global visibility without editing',['global','current','enabled'],'participation:beer-2027','view_schedule',True),
             ('global has no implicit edit',['global','current','enabled'],'shift:setup','edit_schedule',False),
             ('explicit self grant permits edit',['global','team','enabled'],'shift:setup','edit_schedule',True),
             ('old team alone grants no view',['team','enabled'],'shift:setup','view_schedule',False),
             ('current team edits',['team','current','enabled'],'shift:setup','edit_schedule',True),
             ('disabled global read',['global','team','current'],'shift:setup','view_schedule',True),
             ('disabled global cannot edit',['global','team','current'],'shift:setup','edit_schedule',False),
             ('member sees current roster',['member','current','enabled'],'shift:setup','view_schedule',True),
             ('member cannot edit',['member','current','enabled'],'shift:setup','edit_schedule',False),
             ('member cannot browse old roster',['member','enabled'],'shift:setup','view_schedule',False),
             ('guest capability permits signup',['signup','current','enabled'],'shift:setup','request_place',True),
             ('guest capability reveals no roster',['signup','current','enabled'],'shift:setup','view_schedule',False),
             ('guest capability cannot target another shift',['signup','current','enabled'],'shift:other','request_place',False),
             ('expired current gate stops signup',['signup','enabled'],'shift:setup','request_place',False),
             ('own archived request remains visible',['owner'],'request:one','view',True),
             ('own archived request cannot withdraw',['owner'],'request:one','withdraw',False),
             ('own current pending request withdraw',['owner','current','enabled'],'request:one','withdraw',True),
             ('own disabled request read only',['owner','current'],'request:one','withdraw',False),
             ('member cannot view other pending requests',['member','current','enabled'],'request:one','view',False),
             ('current organizer manages member worker',['team','current','enabled'],'worker:anna','edit_contacts',True),
             ('old organizer cannot manage shared worker',['team','enabled'],'worker:anna','edit_contacts',False),
             ('worker cannot edit own notes',['owner'],'worker:anna','edit_notes',False),
             ('worker may read own notes',['owner'],'worker:anna','view_details',True),
             ('revoked team immediately loses edit',['current','enabled'],'shift:setup','edit_schedule',False),
             ('operator unlink removes private access',[],'worker:anna','view_details',False)]
            for name,keys,obj,relation,want in cases:
                context=basefacts+[facts[k] for k in keys]
                assert len(context)<=100
                result=post(prefix+'/check',{'authorization_model_id':model,'tuple_key':tuple_('principal:alice',relation,obj),'contextual_tuples':{'tuple_keys':context},'consistency':'HIGHER_CONSISTENCY'})
                assert result['allowed']==want, (name,result,want)
                print('PASS',name)
            print(f'{len(cases)} engine checks passed. No application/database concurrency or browser behavior is asserted here.')
        finally:
            server.terminate()
            try:server.wait(timeout=5)
            except subprocess.TimeoutExpired:server.kill();server.wait()
if __name__=='__main__':
    args=argparse.ArgumentParser(description=__doc__);args.add_argument('--binary',required=True);run(str(Path(args.parse_args().binary).resolve()))
