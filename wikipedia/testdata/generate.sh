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

for f in s0 s1 s2; do
    bzip2 -kf $f.xml
done
cat s0.xml.bz2 s1.xml.bz2 s2.xml.bz2 > fixture-articles.xml.bz2

o1=$(stat -c%s s0.xml.bz2)
o2=$((o1 + $(stat -c%s s1.xml.bz2)))

cat > index.txt <<EOF
$o1:12:Anarchism
$o1:25:Autism
$o1:42:Category:Test
$o2:736:Albert Einstein
$o2:999:Einstein
$o2:1000:Ω
EOF
bzip2 -zf index.txt
mv index.txt.bz2 fixture-index.txt.bz2

rm -f s0.xml s1.xml s2.xml s0.xml.bz2 s1.xml.bz2 s2.xml.bz2
echo "Fixtures written: fixture-index.txt.bz2 fixture-articles.xml.bz2 (streams at 0, $o1, $o2)"
