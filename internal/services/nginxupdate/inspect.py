"""Read-only libapt policy inspection; no resolver commit or package writes."""
import hashlib
import json
import os
import re
import sys
import urllib.parse


def policy_identity():
    import apt_pkg
    apt_pkg.init_config()
    locations = [apt_pkg.config.find_file(key) for key in ('Dir::Etc::sourcelist', 'Dir::Etc::preferences', 'Dir::Etc::main')]
    locations += [apt_pkg.config.find_dir(key) for key in ('Dir::Etc::sourceparts', 'Dir::Etc::preferencesparts', 'Dir::Etc::parts')]
    digest = hashlib.sha256()
    count = 0
    total = 0
    for base in sorted(set(locations)):
        paths = [os.path.join(base, n) for n in sorted(os.listdir(base))] if os.path.isdir(base) else [base]
        for path in paths:
            if not os.path.isfile(path):
                continue
            count += 1
            if count > 512 or os.path.getsize(path) > 1048576:
                raise ValueError('APT policy exceeds inspection limits')
            with open(path, 'rb') as stream:
                value = stream.read(1048577)
            if len(value) > 1048576:
                raise ValueError('APT policy changed during inspection')
            total += len(value)
            if total > 4194304:
                raise ValueError('APT policy exceeds inspection limits')
            digest.update(path.encode() + b'\x00' + hashlib.sha256(value).digest())
    return digest.hexdigest()


def inspect(cache, version_compare, name):
    if not re.fullmatch(r'nginx(?:-[a-z0-9][a-z0-9+.-]*)?(?::[a-z0-9][a-z0-9-]*)?', name):
        raise ValueError('Unknown Nginx binary package')
    if name not in cache or cache[name].installed is None:
        raise ValueError('Binary owner is not an installed APT package')
    package = cache[name]
    installed, candidate = package.installed, package.candidate
    held = package._pkg.selected_state == 2  # apt_pkg.SELSTATE_HOLD
    result = dict(package=package.name, architecture=installed.architecture,
                  installed_version=installed.version, candidate_version='', sources=[], held=held,
                  state='unavailable', reason='No downloadable candidate is offered by the configured APT policy.')
    if candidate is None:
        return result
    result['candidate_version'] = candidate.version
    sources = []
    for origin in candidate.origins:
        # The status-file pseudo-origin is not a repository offering.
        if not origin.site and not origin.origin:
            continue
        host = urllib.parse.urlsplit('//' + origin.site).hostname or ''
        sources.append(dict(origin=origin.origin[:256], archive=origin.archive[:256],
                            site=host[:256], component=origin.component[:128], trusted=bool(origin.trusted)))
    result['sources'] = sources
    if not sources or not candidate.downloadable or not all(o['trusted'] for o in sources):
        return result
    comparison = version_compare(candidate.version, installed.version)
    if comparison < 0:
        result['reason'] = 'Current APT policy offers an older revision. No downgrade is recommended.'
    elif comparison == 0:
        result.update(state='current', reason='No newer revision is offered by the configured APT policy.')
    elif held:
        result.update(state='held', reason='The binary package is held. Review the hold over SSH; Fluxo does not change it.')
    else:
        result.update(state='update_available', reason='A newer revision is offered by the configured APT policy.')
    return result


def main():
    request = json.load(sys.stdin)
    if request.get('action') == 'policy':
        print('FLUXO_NGINX_JSON:' + json.dumps(dict(policy=policy_identity())))
        return
    import apt
    import apt_pkg
    apt_pkg.init()
    apt_pkg.config.set('Dir::Cache::pkgcache', '')
    apt_pkg.config.set('Dir::Cache::srcpkgcache', '')
    before = policy_identity()
    result = inspect(apt.Cache(), apt_pkg.version_compare, request['package'])
    if policy_identity() != before:
        raise ValueError('APT policy changed during inspection')
    result['source_identity'] = before
    print('FLUXO_NGINX_JSON:' + json.dumps(result))


if __name__ == '__main__':
    main()
