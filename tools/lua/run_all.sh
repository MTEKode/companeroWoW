#!/bin/sh
# run_all.sh: ejecuta todas las suites Lua del addon desde la raiz del repo y comprueba que
# compilan (luajit -bl) todos los ficheros de CompaneroWoW/. Sale con codigo != 0 si algo falla.
#   sh tools/lua/run_all.sh
cd "$(dirname "$0")/../.." || exit 1
fail=0
for f in CompaneroWoW/*.lua; do
    if ! luajit -bl "$f" > /dev/null 2>&1; then
        echo "SYNTAX FAIL  $f"
        fail=1
    fi
done
for t in tools/lua/test_*.lua; do
    out=$(luajit "$t" 2>&1)
    rc=$?
    if [ $rc -ne 0 ] || ! echo "$out" | grep -q "ALL TESTS PASSED"; then
        echo "SUITE FAIL   $t (rc=$rc)"
        echo "$out" | grep -E "FAIL|rror" | head -5
        fail=1
    else
        echo "suite ok     $t"
    fi
done
exit $fail
