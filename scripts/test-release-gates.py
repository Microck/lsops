import os,pathlib,subprocess,tempfile,textwrap
workflow=(pathlib.Path(__file__).resolve().parents[1]/'.github/workflows/release.yml').read_text()
body=textwrap.dedent(workflow.rsplit('        run: |\n',1)[1])
# Stop immediately before the download stage; mock release creation ends successfully.
body=body[:body.index('mkdir "$RUNNER_TEMP/downloaded"')]
cases=[
 ('chore(release): prepare v0.5.0','',False,True),
 ('chore(release): prepare v0.5.0','expected',False,True),
 ('chore(release): prepare v0.5.0 revised','',False,False),
 ('chore(release): prepare v0.5.0','elsewhere',False,False),
 ('chore(release): prepare v0.5.0','expected',True,False),
 ('chore(release): prepare v0.5.0','NETWORK_ERROR',False,False),
]
with tempfile.TemporaryDirectory() as tmp:
 root=pathlib.Path(tmp)
 for name,text in {
 'git': '#!/bin/bash\nif [ "$1" = log ]; then printf "%s\\n" "$SUBJECT"; else [ "$TAG" != NETWORK_ERROR ] || exit 9; [ -z "$TAG" ] || printf "%s\\trefs/tags/v0.5.0\\n" "$TAG"; fi\n',
 'gh': '#!/bin/bash\nif [ "$1 $2" = "release view" ]; then [ "$EXISTS" = yes ]; elif [ "$1 $2" = "release create" ]; then echo created >> "$MARKER"; else echo tag >> "$MARKER"; fi\n'
 }.items():
  p=root/name;p.write_text(text);p.chmod(0o755)
 for i,(subject,tag,exists,ok) in enumerate(cases):
  marker=root/str(i)
  env=dict(os.environ,PATH=str(root)+':'+os.environ['PATH'],SUBJECT=subject,TAG=tag,EXISTS='yes' if exists else 'no',MARKER=str(marker),GITHUB_SHA='expected',GITHUB_REPOSITORY='Microck/lsops',RUNNER_TEMP=tmp)
  result=subprocess.run(['bash','-c',body],env=env,capture_output=True,text=True)
  created=marker.exists() and 'created' in marker.read_text()
  assert created==ok,(i,result.returncode,result.stdout,result.stderr)
 print('Publication gates passed: fresh tag, matching-tag retry, suffix rejection, mismatched-tag rejection, existing-release rejection')
