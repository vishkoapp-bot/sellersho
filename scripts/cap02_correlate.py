#!/usr/bin/env python3
"""CAP-02 correlation report (W-2.2).

Builds the per-run appendix the spec requires — route latency joined
to SQL / locks / pool / Redis / edge / provider / CPU / memory — from
the runner's own /metrics snapshots + pg_stat_statements + run
manifests. No Prometheus scrape is required: the runner writes
<profile>-metrics-before.prom / -after.prom for every profile, so the
collector works even while the prod Prometheus backend scrape is DOWN
(PERF-03 C2; see the discrete follow-up item in
docs/operations/capacity-evidence.txt §4 — do NOT fix the 9090 publish
here).

Usage:
    python3 scripts/cap02_correlate.py --run-dir <run-output-dir>
        [--pgstat <tsv>] [--out <dir>] [--proof otp-shedder
        [--expected-ok N] [--expected-503 N] [--expected-rate F]]

--pgstat is an optional pg_stat_statements dump, one row per line, TSV:
    query<TAB>calls<TAB>mean_exec_time_ms<TAB>total_exec_time_ms<TAB>rows
Without it the SQL section is limited to the operation counters in
/metrics and says so explicitly — it never invents statement text.

--proof otp-shedder reproduces one known CAP-01 finding from raw data
before the collector is trusted on new runs (audit §8 W-2.2): the OTP
shedder 40.7% — login-ramp otp_send 486 OK / 334x503 in the 0587 clean
run. It recomputes the counts from the /metrics deltas in the run dir
and compares them with the expected values (defaults: the 0587
numbers). Exit 0 on match (rate within 0.005), 1 otherwise.

Findings are rule-based and labeled with their evidence; the collector
never asserts a cause it cannot see (e.g. it reports "pool waits
observed" with counts, not "pool waits caused the latency").
"""
import argparse
import json
import pathlib
import re
import sys

LABEL_RE = re.compile(r'([a-zA-Z_][a-zA-Z0-9_]*)="((?:[^"\\]|\\.)*)"')
LINE_RE = re.compile(r'^([a-zA-Z_:][a-zA-Z0-9_:]*)(\{[^}]*\})?\s+([-+0-9.eEnaif]+)$')


# Driver route names (load-tests/*.mjs route_metrics keys) map to the
# ServeMux route patterns the backend records in /metrics
# (sellersho_http_duration_seconds_* route label). Verified against
# load-tests/commerce-baseline.mjs paths + the 0587 metrics snapshots.
ROUTE_MAP = {
    'list': '/api/v1/products',
    'detail': '/api/v1/products/*',
    'store': '/api/v1/stores/*',
    'account': '/api/v1/auth/me',
    'quote': '/api/v1/shipping/quote',
    'create': '/api/v1/orders',
    'initiate': '/api/v1/payments/shaparak/token',
    'callback': '/api/v1/payments/shaparak/callback',
    'otp_send': '/api/v1/auth/otp/send',
    'otp_verify': '/api/v1/auth/otp/verify',
}


def parse_prometheus(text):
    """Parse Prometheus text exposition into {(name, frozenset labels): value}."""
    series = {}
    for line in text.splitlines():
        line = line.strip()
        if not line or line.startswith('#'):
            continue
        m = LINE_RE.match(line)
        if not m:
            continue
        name, label_text, raw = m.group(1), m.group(2), m.group(3)
        if raw in ('+Inf', 'Inf'):
            value = float('inf')
        elif raw == '-Inf':
            value = float('-inf')
        elif raw == 'NaN':
            value = float('nan')
        else:
            try:
                value = float(raw)
            except ValueError:
                continue
        labels = {}
        if label_text:
            for lm in LABEL_RE.finditer(label_text[1:-1]):
                labels[lm.group(1)] = lm.group(2).replace('\\"', '"').replace('\\\\', '\\')
        series[(name, tuple(sorted(labels.items())))] = value
    return series


def labels_of(key):
    return dict(key[1])


def delta(before, after):
    """Counter deltas (after - before); gauges are reported as after-values
    by the caller. Missing-before counts as 0; missing-after means the
    series vanished — report None, never silently 0."""
    out = {}
    for key, aval in after.items():
        bval = before.get(key)
        if bval is None:
            out[key] = aval
        else:
            out[key] = aval - bval
    return out


def sum_by_label(series, name, group_label):
    totals = {}
    for (mname, labels), value in series.items():
        if mname != name:
            continue
        d = dict(labels)
        totals[d.get(group_label, '')] = totals.get(d.get(group_label, ''), 0) + value
    return totals


def http_counts(deltas):
    """Per (route, status) request counts from http duration _count deltas."""
    counts = {}
    for (mname, labels), value in deltas.items():
        if mname != 'sellersho_http_duration_seconds_count':
            continue
        d = dict(labels)
        counts[(d.get('route', ''), d.get('status', ''))] = value
    return counts


def operation_counts(deltas):
    """Per (component, operation, outcome) op counts."""
    counts = {}
    for (mname, labels), value in deltas.items():
        if mname != 'sellersho_operation_duration_seconds_count':
            continue
        d = dict(labels)
        counts[(d.get('component', ''), d.get('operation', ''),
                d.get('outcome', ''))] = value
    return counts


def operation_sums(deltas):
    sums = {}
    for (mname, labels), value in deltas.items():
        if mname != 'sellersho_operation_duration_seconds_sum':
            continue
        d = dict(labels)
        sums[(d.get('component', ''), d.get('operation', ''),
              d.get('outcome', ''))] = value
    return sums


def gauge_after(after, name):
    vals = {}
    for (mname, labels), value in after.items():
        if mname == name:
            vals[labels] = value
    return vals


def single_delta(deltas, name):
    total = 0.0
    found = False
    for (mname, _labels), value in deltas.items():
        if mname == name:
            total += value
            found = True
    return total if found else None


def correlate_profile(run_dir, profile):
    before_path = run_dir / (profile + '-metrics-before.prom')
    after_path = run_dir / (profile + '-metrics-after.prom')
    if not before_path.exists() or not after_path.exists():
        return None
    before = parse_prometheus(before_path.read_text())
    after = parse_prometheus(after_path.read_text())
    d = delta(before, after)
    summary = None
    summary_path = run_dir / (profile + '-summary.json')
    if summary_path.exists():
        try:
            summary = json.loads(summary_path.read_text())
        except Exception:
            summary = None
    manifest = {}
    manifest_path = run_dir / 'manifest.json'
    if manifest_path.exists():
        try:
            manifest = json.loads(manifest_path.read_text())
        except Exception:
            manifest = {}
    elapsed = summary.get('elapsed_seconds') if summary else None
    httpc = http_counts(d)
    opc = operation_counts(d)
    ops = operation_sums(d)
    total_http = sum(v for v in httpc.values())
    err_http = sum(v for (r, s), v in httpc.items() if s.startswith('5'))
    shedder = sum(v for (c, o, oc), v in opc.items()
                  if c == 'provider' and oc == 'overloaded')
    sql_ops = {k: v for k, v in opc.items() if k[0] == 'sql'}
    sql_err = sum(v for (c, o, oc), v in opc.items()
                  if c == 'sql' and oc not in ('success',))
    db_wait_total = single_delta(d, 'sellersho_db_wait_total')
    db_wait_seconds = single_delta(d, 'sellersho_db_wait_seconds_total')
    deadlocks = single_delta(d, 'sellersho_pg_deadlocks_total')
    pg_tx = single_delta(d, 'sellersho_pg_transactions_total')
    redis_miss = single_delta(d, 'sellersho_redis_pool_misses_total')
    redis_timeout = single_delta(d, 'sellersho_redis_pool_timeouts_total')
    cpu_seconds = single_delta(d, 'sellersho_process_cpu_seconds_total')
    heap_after = gauge_after(after, 'sellersho_go_heap_bytes')
    heap_value = next(iter(heap_after.values()), None)
    backlogs = {}
    for (mname, labels), value in after.items():
        if mname.startswith('sellersho_worker_') and mname.endswith('_backlog'):
            backlogs[mname[len('sellersho_worker_'):-len('_backlog')]] = value
    findings = []
    if shedder:
        findings.append({
            'rule': 'provider_overload_shedder',
            'detail': 'provider operations with outcome=overloaded: %d '
                      '(IPPanel dependency-budget shedder; 503s are shed, '
                      'not backend failures)' % shedder})
    if summary and summary.get('dropped_iterations'):
        findings.append({
            'rule': 'dropped_arrivals',
            'detail': 'driver dropped %d of %d offered iterations '
                      '(%.1f%%) — the generator could not deliver the '
                      'offered rate on this host; delivered RPS is a host '
                      'ceiling, not an app measurement' % (
                          summary['dropped_iterations'], summary.get('offered', 0),
                          100.0 * summary['dropped_iterations'] / max(summary.get('offered', 1), 1))})
    if db_wait_total:
        mean_wait_ms = (1000.0 * (db_wait_seconds or 0) / db_wait_total
                        if db_wait_total else 0)
        findings.append({
            'rule': 'pool_waits',
            'detail': 'pool waits: %d events, %.3f s cumulative '
                      '(mean %.1f ms)' % (db_wait_total, db_wait_seconds or 0,
                                          mean_wait_ms)})
    if deadlocks:
        findings.append({'rule': 'deadlocks',
                         'detail': 'PostgreSQL deadlocks in window: %d' % deadlocks})
    if redis_timeout:
        findings.append({'rule': 'redis_pool_timeouts',
                         'detail': 'Redis pool timeouts in window: %d' % redis_timeout})
    nonzero_backlogs = {k: v for k, v in backlogs.items() if v}
    if nonzero_backlogs:
        findings.append({'rule': 'worker_backlog',
                         'detail': 'worker backlogs after profile: %s'
                                   % json.dumps(nonzero_backlogs)})
    if sql_err:
        findings.append({'rule': 'sql_operation_errors',
                         'detail': 'SQL operations with non-success outcome: %d' % sql_err})
    routes = []
    if summary and summary.get('route_metrics'):
        for route, m in summary['route_metrics'].items():
            pattern = ROUTE_MAP.get(route, route)
            rc = {s: v for (r, s), v in httpc.items() if r == pattern}
            routes.append({
                'route': route,
                'metric_route': pattern,
                'driver_p95_ms': m.get('p95_ms'),
                'driver_p99_ms': m.get('p99_ms'),
                'http_counts_by_status': rc,
                'http_5xx': sum(v for s, v in rc.items() if s.startswith('5')),
            })
    return {
        'profile': profile,
        'elapsed_seconds': elapsed,
        'exit': manifest.get(profile + '_exit'),
        'cpu_max_percent': manifest.get(profile + '_cpu_max_percent'),
        'counter_gate': manifest.get(profile + '_counter_gate'),
        'oracle_gate': manifest.get(profile + '_oracle_gate'),
        'received_counts': manifest.get(profile + '_received_counts'),
        'driver': ({k: summary.get(k) for k in (
            'offered', 'requests', 'dropped_iterations', 'errors',
            'failed_gates', 'completed_checkouts', 'actual_api_rps',
            'actual_attempted_api_rps', 'actual_response_api_rps')}
            if summary else None),
        'http': {'total': total_http, 'http_5xx': err_http,
                 'by_route_status': {'%s %s' % k: v for k, v in sorted(httpc.items())}},
        'provider': {'overloaded_ops': shedder,
                     'ops_by_outcome': {'%s/%s/%s' % k: v for k, v in sorted(opc.items())
                                        if k[0] == 'provider'}},
        'sql': {'ops_by_outcome': {'%s/%s/%s' % k: v for k, v in sorted(sql_ops.items())},
                'total_seconds_by_outcome': {'%s/%s/%s' % k: v for k, v in sorted(ops.items())
                                             if k[0] == 'sql'}},
        'pool': {'db_wait_total': db_wait_total,
                 'db_wait_seconds_total': db_wait_seconds,
                 'pg_transactions_total': pg_tx,
                 'pg_deadlocks_total': deadlocks},
        'redis': {'pool_misses_total': redis_miss,
                  'pool_timeouts_total': redis_timeout},
        'cpu_memory': {'process_cpu_seconds_total': cpu_seconds,
                       'avg_cpu_percent': (100.0 * cpu_seconds / elapsed
                                           if cpu_seconds is not None and elapsed else None),
                       'go_heap_bytes_after': heap_value},
        'workers': {'backlog_after': backlogs},
        'routes': routes,
        'findings': findings,
    }


def load_pgstat(path):
    """pg_stat_statements TSV: query, calls, mean_ms, total_ms, rows."""
    rows = []
    for line in pathlib.Path(path).read_text().splitlines():
        if not line.strip():
            continue
        parts = line.split('\t')
        if len(parts) < 5:
            continue
        try:
            rows.append({'query': parts[0][:200], 'calls': int(parts[1]),
                         'mean_exec_time_ms': float(parts[2]),
                         'total_exec_time_ms': float(parts[3]),
                         'rows': int(parts[4])})
        except ValueError:
            continue
    rows.sort(key=lambda r: r['total_exec_time_ms'], reverse=True)
    return rows[:20]


def proof_otp_shedder(run_dir, expected_ok, expected_503, expected_rate):
    """Reproduce the OTP shedder 40.7% from raw /metrics deltas alone.
    Uses the login-ramp profile's before/after snapshots: otp/send
    status counts from the http histogram + the provider overloaded
    operation count. Returns (ok, detail dict)."""
    profile = correlate_profile(run_dir, 'login-ramp')
    if profile is None:
        return False, {'error': 'login-ramp metrics snapshots not found in run dir'}
    httpc = profile['http']['by_route_status']
    ok = httpc.get('/api/v1/auth/otp/send 200', 0)
    shed = httpc.get('/api/v1/auth/otp/send 503', 0)
    overloaded = profile['provider']['overloaded_ops']
    total = ok + shed
    rate = shed / total if total else 0.0
    detail = {'metrics_otp_send_200': ok, 'metrics_otp_send_503': shed,
              'metrics_provider_overloaded': overloaded,
              'shed_rate': round(rate, 4),
              'expected_ok': expected_ok, 'expected_503': expected_503,
              'expected_rate': expected_rate,
              'driver_requests': (profile['driver'] or {}).get('requests'),
              'driver_route_metrics': None}
    match = (ok == expected_ok and shed == expected_503
             and abs(rate - expected_rate) <= 0.005
             and overloaded == expected_503)
    detail['match'] = match
    if not match:
        detail['mismatch'] = ('metrics deltas give %d OK / %d x503 '
                              '(rate %.4f, overloaded ops %d); expected '
                              '%d OK / %d x503 (rate %.4f)'
                              % (ok, shed, rate, overloaded,
                                 expected_ok, expected_503, expected_rate))
    return match, detail


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--run-dir', type=pathlib.Path, required=True,
                        help='runner output dir (manifest.json + *-metrics-*.prom + *-summary.json)')
    parser.add_argument('--pgstat', type=pathlib.Path, default=None,
                        help='optional pg_stat_statements TSV dump')
    parser.add_argument('--out', type=pathlib.Path, default=None,
                        help='directory for the JSON appendix (default: stdout only)')
    parser.add_argument('--proof', choices=['otp-shedder'], default=None)
    parser.add_argument('--expected-ok', type=int, default=486)
    parser.add_argument('--expected-503', type=int, default=334)
    parser.add_argument('--expected-rate', type=float, default=0.407)
    args = parser.parse_args()

    run_dir = args.run_dir
    if not run_dir.is_dir():
        print('run dir not found: %s' % run_dir, file=sys.stderr)
        return 3
    profiles = sorted({p.name[:-len('-metrics-before.prom')]
                       for p in run_dir.glob('*-metrics-before.prom')})
    appendix = {'run_dir': str(run_dir), 'profiles': [],
                'pg_stat_statements': None}
    for profile in profiles:
        entry = correlate_profile(run_dir, profile)
        if entry is not None:
            appendix['profiles'].append(entry)
    if args.pgstat:
        appendix['pg_stat_statements'] = {
            'source': str(args.pgstat),
            'top_by_total_time_ms': load_pgstat(args.pgstat)}
    else:
        appendix['pg_stat_statements'] = {
            'source': None,
            'note': 'pg_stat_statements not supplied — SQL attribution is '
                    'limited to the /metrics operation counters; statement '
                    'text is never invented'}
    if args.out:
        args.out.mkdir(parents=True, exist_ok=True)
        (args.out / 'cap02-correlation.json').write_text(json.dumps(appendix, indent=2))
    for entry in appendix['profiles']:
        print('== %s (exit %s, cpu_max %s%%) ==' % (
            entry['profile'], entry['exit'], entry['cpu_max_percent']))
        if entry['driver']:
            d = entry['driver']
            print('   driver: offered=%s requests=%s dropped=%s errors=%s '
                  'failed_gates=%s' % (d.get('offered'), d.get('requests'),
                                       d.get('dropped_iterations'), d.get('errors'),
                                       d.get('failed_gates')))
        print('   http: total=%s 5xx=%s | provider overloaded ops=%s | '
              'pool waits=%s (%.3fs) | deadlocks=%s | redis timeouts=%s' % (
                  entry['http']['total'], entry['http']['http_5xx'],
                  entry['provider']['overloaded_ops'], entry['pool']['db_wait_total'],
                  entry['pool']['db_wait_seconds_total'] or 0,
                  entry['pool']['pg_deadlocks_total'],
                  entry['redis']['pool_timeouts_total']))
        for route in entry['routes']:
            print('   route %-10s p95=%s p99=%s 5xx=%s counts=%s' % (
                route['route'], route['driver_p95_ms'], route['driver_p99_ms'],
                route['http_5xx'], route['http_counts_by_status']))
        for f in entry['findings']:
            print('   FINDING [%s] %s' % (f['rule'], f['detail']))
    if args.proof == 'otp-shedder':
        ok, detail = proof_otp_shedder(run_dir, args.expected_ok,
                                       args.expected_503, args.expected_rate)
        print('PROOF otp-shedder: %s' % ('MATCH' if ok else 'MISMATCH'))
        print('  ' + json.dumps(detail, indent=2))
        return 0 if ok else 1
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
