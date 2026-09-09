import json
import os
import pathlib
import shutil
import subprocess
import sys
import tempfile
ROOT = pathlib.Path(__file__).resolve().parents[1]
NAME = sys.argv[1]
META = json.loads((ROOT / 'extras' / NAME / 'demo.json').read_text())
TOOL = META['core']
OLD = 'package auth\nimport "strings"\nfunc ValidToken(header string) bool { return strings.Contains(header, "Bearer ") }\n'
NEW = 'package auth\nimport "strings"\nfunc ValidToken(header string) bool {\n if !strings.HasPrefix(header, "Bearer ") { return false }\n token := strings.TrimPrefix(header, "Bearer ")\n return token != "" && !strings.ContainsAny(token, " \\t\\n")\n}\n'
TEST = 'package auth\nimport "testing"\nfunc TestTokenBoundary(t *testing.T) {\n for _, tc := range []struct{header string; valid bool}{\n {"Bearer signed-token",true},{"",false},{"Bearer ",false},{"prefix Bearer token",false},{"Bearer two tokens",false},\n } { if got:=ValidToken(tc.header); got!=tc.valid {t.Errorf("%q: got %v want %v",tc.header,got,tc.valid)} }\n}\n'

def execute(argv, cwd, env, stdin=None, show=True, check=True):
    if show:
        display = [pathlib.Path(str(argv[0])).name, *[str(value).replace(str(cwd), '.') for value in argv[1:]]]
        print('$ ' + ' '.join(display), flush=True)
    result = subprocess.run([str(value) for value in argv], cwd=cwd, env=env, input=stdin, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=90)
    if show or result.returncode:
        print(result.stdout.rstrip().replace(str(cwd), '.'), flush=True)
    if check and result.returncode:
        raise RuntimeError('command failed: ' + str(result.returncode))
    return result

def build(package, binary, bins):
    module = ROOT
    source = ROOT / package.removeprefix('./')
    if (source / 'go.mod').exists():
        module, package = (source, '.')
    target = bins / binary
    execute(['go', 'build', '-o', target, package], module, os.environ.copy(), show=False)
    return str(target)

def shim(bins, name, body):
    target = bins / name
    target.write_text('#!/usr/bin/env python3\n' + body)
    target.chmod(493)

def demo(work, env, bins):
    binary = build('./extras/' + NAME, META['binary'], bins)
    path = work / 'internal/auth/token.go'

    def event(kind, tool='', data=None, args=None):
        request = {'version': 'provider/v1', 'kind': 'request', 'requestId': 'token-review', 'capability': 'gate.decide', 'input': {'event': {'version': 'gate.event/v1', 'event': kind, 'tool': tool, 'cwd': str(work), 'input': data or {}, 'session': 'token-review'}, 'args': args or {}}}
        print(kind + (' ' + tool if tool else ''), flush=True)
        result = execute([binary], work, env, json.dumps(request), show=False)
        frame = json.loads(result.stdout)
        if frame.get('status') != 'ok':
            raise RuntimeError(frame.get('message'))
        outcome = frame['output']
        print(outcome['decision'] + ': ' + outcome.get('message', '').replace(str(work), '.'), flush=True)
    if NAME in {'bash-guard', 'read-router'}:
        path.write_text(NEW + '// Token validation contract. ' * 8 + '\n' + '// Reject invalid authorization headers before looking up the session.\n' * 250)
        if NAME == 'bash-guard':
            rules = work / 'rules.json'
            rules.write_text(json.dumps([{'regex': '^deploy ', 'reason': 'Review deployment before publishing'}]))
            event('PreToolUse', 'Bash', {'command': 'cat internal/auth/token.go'}, {'rules': str(rules)})
            event('PreToolUse', 'Bash', {'command': 'sed -n 1,12p internal/auth/token.go'}, {'rules': str(rules)})
        else:
            event('PreToolUse', 'Read', {'file_path': str(path)})
            event('PreToolUse', 'Read', {'file_path': str(path), 'offset': 1, 'limit': 12})
    elif NAME == 'nix-guard':
        target = pathlib.Path(shutil.which('go')).resolve()
        link = work / 'managed-config.nix'
        link.symlink_to(target)
        event('PreToolUse', 'Write', {'file_path': str(link), 'content': 'configuration change'})
    elif NAME == 'lint-gate':
        event('PostToolUse', 'Write', {'file_path': str(path)})
        execute(['gofmt', '-w', path], work, env)
        event('PostToolUse', 'Write', {'file_path': str(path)})
    elif NAME == 'loop-gate':
        path.write_text(OLD)
        execute([binary, 'arm', '--until', 'go test ./internal/auth', '--max', '3', '--stall', '2'], work, env)
        event('Stop')
        path.write_text(NEW)
        event('Stop')
    elif NAME == 'notes':
        package = subprocess.check_output(['nix', 'build', 'github:roshbhatia/agent-notes', '--no-link', '--print-out-paths'], cwd=ROOT, text=True, stderr=subprocess.PIPE).strip()
        env['PATH'] = package + '/bin:' + env['PATH']
        execute(['note', 'add', '--file', path, '--line', '4', '--origin', 'user', '--summary', 'Why must prefix validation run before session lookup?'], work, env)
        event('UserPromptSubmit')
    elif NAME == 'edit-event':
        event('UserPromptSubmit', data={'prompt': 'Reject malformed authorization headers'})
        event('PostToolUse', 'Write', {'file_path': str(path)})
        execute(['git', 'diff', '--stat'], work, env)
    elif NAME in {'review', 'review-gate'}:
        review = binary if NAME == 'review' else build('./extras/review', 'review', bins)
        execute([review, 'open', '--base', 'HEAD', '--change', 'token-parser'], work, env)
        if NAME == 'review-gate':
            event('PreToolUse', 'Agent', {'prompt': 'ADVERSARIAL-CRITIC-ROLE: Review token validation', 'subagent_type': 'Explore'})
    elif NAME == 'prose-gate':
        styles = work / 'styles/Release'
        styles.mkdir(parents=True)
        (styles / 'Words.yml').write_text("extends: existence\nmessage: 'Use fixed.'\nlevel: error\ntokens:\n  - leveraged\n")
        ini = work / 'vale.ini'
        ini.write_text('StylesPath = styles\n[*.md]\nBasedOnStyles = Release\n')
        execute([binary, 'lint', '--style', ini], work, env, 'We leveraged a new parser.\n', check=False)
        execute([binary, 'lint', '--style', ini], work, env, 'We fixed token validation.\n')

def main():
    with tempfile.TemporaryDirectory(prefix='token-review-') as temporary:
        root = pathlib.Path(temporary).resolve()
        work = root / 'checkout-service'
        bins = root / 'bin'
        bins.mkdir()
        (work / 'internal/auth').mkdir(parents=True)
        (work / 'go.mod').write_text('module checkout-service\n\ngo 1.26\n')
        path = work / 'internal/auth/token.go'
        path.write_text(OLD)
        (work / 'internal/auth/token_test.go').write_text(TEST)
        env = os.environ.copy()
        for name, dirname in [('HOME', 'home'), ('XDG_CONFIG_HOME', 'config'), ('XDG_DATA_HOME', 'data'), ('XDG_STATE_HOME', 'state'), ('XDG_CACHE_HOME', 'cache'), ('XDG_RUNTIME_DIR', 'runtime')]:
            (root / dirname).mkdir()
            env[name] = str(root / dirname)
        env['PATH'] = str(bins) + os.pathsep + env['PATH']
        env['XDG_DATA_DIRS'] = str(root / 'data')
        for name in ['ORC_SESSION_ID', 'ORC_SCOPE', 'WEZTERM_PANE', 'WEZTERM_UNIX_SOCKET', 'GATE_STATE_DIR']:
            env.pop(name, None)
        execute(['git', 'init', '-b', 'main'], work, env, show=False)
        execute(['git', 'config', 'user.name', 'Review fixture'], work, env, show=False)
        execute(['git', 'config', 'user.email', 'review@example.invalid'], work, env, show=False)
        execute(['git', 'add', '.'], work, env, show=False)
        execute(['git', 'commit', '-m', 'add token parser'], work, env, show=False)
        path.write_text(NEW)
        print(META['summary'] + '\n', flush=True)
        demo(work, env, bins)
        print('\nDemo complete', flush=True)
if __name__ == '__main__':
    main()
