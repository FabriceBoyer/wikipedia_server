#!/bin/bash
# Regenerates the multistream dump fixtures used by the Go tests.
# A multistream dump is a concatenation of independent bzip2 streams; the
# index references the byte offset of the stream containing each page.
set -euo pipefail
cd "$(dirname "$0")"

cat > s0.xml <<'EOF'
<mediawiki xmlns="http://www.mediawiki.org/xml/export-0.10/" version="0.10" xml:lang="en">
  <siteinfo>
    <sitename>Test Wiki</sitename>
    <dbname>testwiki</dbname>
  </siteinfo>
EOF

cat > s1.xml <<'EOF'
  <page>
    <title>Anarchism</title>
    <ns>0</ns>
    <id>12</id>
    <revision>
      <id>1001</id>
      <timestamp>2024-01-01T00:00:00Z</timestamp>
      <contributor><username>Tester</username><id>1</id></contributor>
      <model>wikitext</model>
      <format>text/x-wiki</format>
      <text>'''Anarchism''' is a [[political philosophy]] and [[Social movement|movement]].

== History ==
Early currents appeared in the {{lang|grc|arkhē}} era.&lt;ref&gt;A source.&lt;/ref&gt;

* First item
* Second item</text>
    </revision>
  </page>
  <page>
    <title>Autism</title>
    <ns>0</ns>
    <id>25</id>
    <revision>
      <id>1002</id>
      <timestamp>2024-01-02T00:00:00Z</timestamp>
      <contributor><username>Tester</username><id>1</id></contributor>
      <model>wikitext</model>
      <format>text/x-wiki</format>
      <text>'''Autism''' is a neurodevelopmental condition.</text>
    </revision>
  </page>
  <page>
    <title>Category:Test</title>
    <ns>14</ns>
    <id>42</id>
    <revision>
      <id>1003</id>
      <timestamp>2024-01-03T00:00:00Z</timestamp>
      <contributor><username>Tester</username><id>1</id></contributor>
      <model>wikitext</model>
      <format>text/x-wiki</format>
      <text>A category page whose title contains a colon.</text>
    </revision>
  </page>
EOF

cat > s2.xml <<'EOF'
  <page>
    <title>Albert Einstein</title>
    <ns>0</ns>
    <id>736</id>
    <revision>
      <id>2001</id>
      <timestamp>2024-02-01T00:00:00Z</timestamp>
      <contributor><username>Tester</username><id>1</id></contributor>
      <model>wikitext</model>
      <format>text/x-wiki</format>
      <text>'''Albert Einstein''' (1879–1955) was a theoretical [[physicist]] known for [[general relativity]].</text>
    </revision>
  </page>
  <page>
    <title>Einstein</title>
    <ns>0</ns>
    <id>999</id>
    <redirect title="Albert Einstein" />
    <revision>
      <id>2002</id>
      <timestamp>2024-02-02T00:00:00Z</timestamp>
      <contributor><username>Tester</username><id>1</id></contributor>
      <model>wikitext</model>
      <format>text/x-wiki</format>
      <text>#REDIRECT [[Albert Einstein]]</text>
    </revision>
  </page>
  <page>
    <title>Ω</title>
    <ns>0</ns>
    <id>1000</id>
    <revision>
      <id>2003</id>
      <timestamp>2024-02-03T00:00:00Z</timestamp>
      <contributor><username>Tester</username><id>1</id></contributor>
      <model>wikitext</model>
      <format>text/x-wiki</format>
      <text>'''Ω''' is the last letter of the Greek alphabet.</text>
    </revision>
  </page>
</mediawiki>
EOF

# s3: two pages sharing the exact same title with different ids, as the
# real enwiki dump occasionally has (e.g. a stale entry from a page move
# racing the dump snapshot). find() must pick one deterministically
# (highest id) instead of the whole index build failing on a UNIQUE
# violation.
cat > s3.xml <<'EOF'
  <page>
    <title>Duplicate Title</title>
    <ns>0</ns>
    <id>5000</id>
    <revision>
      <id>3001</id>
      <timestamp>2024-03-01T00:00:00Z</timestamp>
      <contributor><username>Tester</username><id>1</id></contributor>
      <model>wikitext</model>
      <format>text/x-wiki</format>
      <text>Stale duplicate (lower id).</text>
    </revision>
  </page>
  <page>
    <title>Duplicate Title</title>
    <ns>0</ns>
    <id>6000</id>
    <revision>
      <id>3002</id>
      <timestamp>2024-03-02T00:00:00Z</timestamp>
      <contributor><username>Tester</username><id>1</id></contributor>
      <model>wikitext</model>
      <format>text/x-wiki</format>
      <text>Current duplicate (higher id, should win).</text>
    </revision>
  </page>
EOF

# s4: one oversized stream (1200 pages) - real multistream dumps nominally
# pack ~100 pages per stream but occasionally pack far more (long runs of
# short redirect stubs). readPage must not give up before reaching the
# last page in a stream this large.
: > s4.xml
for i in $(seq 1 1200); do
    n=$(printf '%04d' "$i")
    id=$((7000 + i))
    cat >> s4.xml <<EOF
  <page>
    <title>Filler $n</title>
    <ns>0</ns>
    <id>$id</id>
    <revision>
      <id>$((90000 + i))</id>
      <timestamp>2024-04-01T00:00:00Z</timestamp>
      <contributor><username>Tester</username><id>1</id></contributor>
      <model>wikitext</model>
      <format>text/x-wiki</format>
      <text>Filler page $n.</text>
    </revision>
  </page>
EOF
done

for f in s0 s1 s2 s3 s4; do
    bzip2 -kf $f.xml
done
cat s0.xml.bz2 s1.xml.bz2 s2.xml.bz2 s3.xml.bz2 s4.xml.bz2 > fixture-articles.xml.bz2

o1=$(stat -c%s s0.xml.bz2)
o2=$((o1 + $(stat -c%s s1.xml.bz2)))
o3=$((o2 + $(stat -c%s s2.xml.bz2)))
o4=$((o3 + $(stat -c%s s3.xml.bz2)))

{
    echo "$o1:12:Anarchism"
    echo "$o1:25:Autism"
    echo "$o1:42:Category:Test"
    echo "$o2:736:Albert Einstein"
    echo "$o2:999:Einstein"
    echo "$o2:1000:Ω"
    echo "$o3:5000:Duplicate Title"
    echo "$o3:6000:Duplicate Title"
    for i in $(seq 1 1200); do
        n=$(printf '%04d' "$i")
        echo "$o4:$((7000 + i)):Filler $n"
    done
} > index.txt
bzip2 -zf index.txt
mv index.txt.bz2 fixture-index.txt.bz2

rm -f s0.xml s1.xml s2.xml s3.xml s4.xml s0.xml.bz2 s1.xml.bz2 s2.xml.bz2 s3.xml.bz2 s4.xml.bz2
echo "Fixtures written: fixture-index.txt.bz2 fixture-articles.xml.bz2 (streams at 0, $o1, $o2, $o3, $o4)"
