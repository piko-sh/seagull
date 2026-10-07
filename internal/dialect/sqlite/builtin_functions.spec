# Copyright 2026 PolitePixels Limited
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# This project stands against fascism, authoritarianism, and all forms of
# oppression. We built this to empower people, not to enable those who would
# strip others of their rights and dignity.

abs(x any) any
length(x any) integer
lower(x text) text
upper(x text) text
trim(x text) text
trim(x text, y text) text
ltrim(x text) text
ltrim(x text, y text) text
rtrim(x text) text
rtrim(x text, y text) text
replace(str text, from text, to text) text
substr(str text, start integer) text
substr(str text, start integer, length integer) text
substring(str text, start integer) text
substring(str text, start integer, length integer) text
hex(x any) text
unhex(x text) blob
instr(str text, substr text) integer
unicode(x text) integer
unistr(x text) text
unistr_quote(x text) text
zeroblob(n integer) blob
round(x real) real
round(x real, y integer) real
sign(x any) integer
typeof(x any) text never-null
quote(x any) text never-null
char(code integer) text never-null variadic=0
printf(format text, value any) text never-null variadic=1
format(format text, value any) text never-null variadic=1
random() bigint never-null
changes() bigint never-null
last_insert_rowid() bigint never-null
total_changes() bigint never-null
nullif(x any, y any) any called-on-null
ifnull(x any, y any) any called-on-null
iif(condition any, x any, rest any) any called-on-null variadic=2
if(condition any, x any, rest any) any called-on-null variadic=2
max(x any, y any) any called-on-null
min(x any, y any) any called-on-null
likelihood(x any, probability real) any called-on-null
likely(x any) any called-on-null
unlikely(x any) any called-on-null

acos(x real) real
asin(x real) real
atan(x real) real
atan2(y real, x real) real
cos(x real) real
sin(x real) real
tan(x real) real
ceil(x any) any
ceiling(x any) any
floor(x any) any
trunc(x any) any
sqrt(x real) real
exp(x real) real
ln(x real) real
log2(x real) real
log10(x real) real
pow(x real, y real) real
power(x real, y real) real
mod(x any, y any) real
degrees(x real) real
radians(x real) real
log(x real) real
log(base real, x real) real
pi() real never-null
acosh(x real) real
asinh(x real) real
atanh(x real) real
cosh(x real) real
sinh(x real) real
tanh(x real) real

date(time any, modifier text) text called-on-null variadic=0
time(time any, modifier text) text called-on-null variadic=0
datetime(time any, modifier text) text called-on-null variadic=0
julianday(time any, modifier text) real called-on-null variadic=0
unixepoch(time any, modifier text) bigint called-on-null variadic=0
strftime(format text, time any, modifier text) text called-on-null variadic=1
timediff(time1 any, time2 any) text called-on-null

glob(pattern text, string text) integer
like(pattern text, string text) integer
like(pattern text, string text, escape text) integer
soundex(x text) text
octet_length(x any) integer
randomblob(n integer) blob
sqlite_version() text never-null
concat(value any) text never-null variadic=1
concat_ws(separator text, value any) text called-on-null variadic=2

json(json text) text
jsonb(json text) blob
json_pretty(json text) text
json_pretty(json text, indent text) text
json_error_position(json text) integer
json_array_length(json text) integer
json_array_length(json text, path text) integer called-on-null
json_type(json text) text
json_type(json text, path text) text called-on-null
json_valid(x text) integer
json_valid(x text, flags integer) integer
json_quote(value any) text never-null
json_extract(json text, path text) any variadic=2
jsonb_extract(json text, path text) any variadic=2
json_array(value any) text never-null variadic=0
json_object(value any) text never-null variadic=0
jsonb_array(value any) blob never-null variadic=0
jsonb_object(value any) blob never-null variadic=0

json_set(json text, path text, value any) text variadic=3
json_insert(json text, path text, value any) text variadic=3
json_replace(json text, path text, value any) text variadic=3
json_remove(json text, path text) text variadic=1
json_patch(json1 text, json2 text) text
jsonb_set(json text, path text, value any) blob variadic=3
jsonb_insert(json text, path text, value any) blob variadic=3
jsonb_replace(json text, path text, value any) blob variadic=3
jsonb_remove(json text, path text) blob variadic=1
jsonb_patch(json1 text, json2 text) blob

json_group_array(value any) text never-null aggregate
json_group_object(name text, value any) text never-null aggregate
jsonb_group_array(value any) blob never-null aggregate
jsonb_group_object(name text, value any) blob never-null aggregate

count() bigint never-null aggregate
count(x any) bigint never-null aggregate
total(x any) real never-null aggregate
avg(x any) real called-on-null aggregate
sum(x any) any called-on-null aggregate
group_concat(x any) text called-on-null aggregate
max(x any) any called-on-null aggregate
min(x any) any called-on-null aggregate
group_concat(x any, separator text) text called-on-null aggregate
string_agg(x any, separator text) text called-on-null aggregate

row_number() bigint never-null
rank() bigint never-null
dense_rank() bigint never-null
ntile(n integer) bigint never-null
cume_dist() real never-null
percent_rank() real never-null

lag(expression any, offset integer, default any) any called-on-null min-arguments=1
lead(expression any, offset integer, default any) any called-on-null min-arguments=1
first_value(expression any) any called-on-null
last_value(expression any) any called-on-null
nth_value(expression any, n integer) any called-on-null

highlight(table text, column_index integer, open_tag text, close_tag text) text called-on-null
snippet(table text, column_index integer, open_tag text, close_tag text, ellipsis text, max_tokens integer) text called-on-null
bm25(table text, weight real) real called-on-null variadic=1
matchinfo(table text) blob called-on-null
matchinfo(table text, format text) blob called-on-null

rtreecheck(table text) text called-on-null
rtreecheck(schema text, table text) text called-on-null
rtreenode(pageno integer, data blob) text called-on-null
