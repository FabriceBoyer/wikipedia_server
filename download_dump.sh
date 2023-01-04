#!/bin/bash
# Common utils
wget_cmd="wget -q --show-progress --limit-rate=1000M"

source $(dirname -- "$0")/.env
out_dir=$DUMP_PATH
mkdir -p $out_dir

# Wikipedia
$wget_cmd https://dumps.wikimedia.org/enwiki/latest/enwiki-latest-pages-articles-multistream.xml.bz2 -O $out_dir/enwiki-pages-articles-multistream.xml.bz2
$wget_cmd https://dumps.wikimedia.org/enwiki/latest/enwiki-latest-pages-articles-multistream-index.txt.bz2 -O $out_dir/enwiki-pages-articles-multistream-index.txt.bz2

# Wiktionary
$wget_cmd https://dumps.wikimedia.org/enwiktionary/latest/enwiktionary-latest-pages-articles-multistream.xml.bz2 -O $out_dir/enwiktionary-pages-articles-multistream.xml.bz2
$wget_cmd https://dumps.wikimedia.org/enwiktionary/latest/enwiktionary-latest-pages-articles-multistream-index.txt.bz2 -O $out_dir/enwiktionary-pages-articles-multistream-index.txt.bz2

# Wikidata
$wget_cmd https://dumps.wikimedia.org/wikidatawiki/entities/latest-all.json.bz2 -O $out_dir/wikidata-all.json.bz2
$wget_cmd https://dumps.wikimedia.org/wikidatawiki/entities/latest-lexemes.json.bz2 -O $out_dir/wikidata-lexemes.json.bz2

# Wikimedia common entities
$wget_cmd https://dumps.wikimedia.org/commonswiki/entities/latest-mediainfo.json.bz2 -O $out_dir/wikimedia-commons-mediainfo.json.bz2

# Commons
$wget_cmd https://dumps.wikimedia.org/commonswiki/latest/commonswiki-latest-pages-articles-multistream.xml.bz2 -O $out_dir/commonswiki-pages-articles-multistream.xml.bz2
$wget_cmd https://dumps.wikimedia.org/commonswiki/latest/commonswiki-latest-pages-articles-multistream-index.txt.bz2 -O $out_dir/commonswiki-pages-articles-multistream-index.txt.bz2

# Wikibooks
$wget_cmd https://dumps.wikimedia.org/enwikibooks/latest/enwikibooks-latest-pages-articles-multistream.xml.bz2 -O $out_dir/enwikibooks-latest-pages-articles-multistream.xml.bz2
$wget_cmd https://dumps.wikimedia.org/enwikibooks/latest/enwikibooks-latest-pages-articles-multistream-index.txt.bz2 -O $out_dir/enwikibooks-latest-pages-articles-multistream-index.txt.bz2

# Wikisource
$wget_cmd https://dumps.wikimedia.org/enwikisource/latest/enwikisource-latest-pages-articles-multistream.xml.bz2  -O $out_dir/enwikisource-pages-articles-multistream.xml.bz2
$wget_cmd https://dumps.wikimedia.org/enwikisource/latest/enwikisource-latest-pages-articles-multistream-index.txt.bz2  -O $out_dir/enwikisource-pages-articles-multistream-index.txt.bz2

# Wikiversity
$wget_cmd https://dumps.wikimedia.org/enwikiversity/latest/enwikiversity-latest-pages-articles-multistream.xml.bz2  -O $out_dir/enwikiversity-pages-articles-multistream.xml.bz2
$wget_cmd https://dumps.wikimedia.org/enwikiversity/latest/enwikiversity-latest-pages-articles-multistream-index.txt.bz2  -O $out_dir/enwikiversity-pages-articles-multistream-index.txt.bz2
