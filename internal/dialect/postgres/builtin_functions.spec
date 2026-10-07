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

abs(x float8) float8
abs(x float4) float4
abs(x int8) int8
abs(x int4) int4
abs(x int2) int2
abs(x numeric) numeric
ceil(x float8) float8
ceil(x numeric) numeric
ceiling(x float8) float8
ceiling(x numeric) numeric
floor(x float8) float8
floor(x numeric) numeric
round(x float8) float8
round(x numeric) numeric
round(x numeric, s int4) numeric
trunc(x float8) float8
trunc(x numeric) numeric
trunc(x numeric, s int4) numeric
sign(x float8) float8
sign(x numeric) numeric
sqrt(x float8) float8
sqrt(x numeric) numeric
cbrt(x float8) float8
power(a float8, b float8) float8
power(a numeric, b numeric) numeric
pow(a float8, b float8) float8
pow(a numeric, b numeric) numeric
exp(x float8) float8
exp(x numeric) numeric
ln(x float8) float8
ln(x numeric) numeric
log(x float8) float8
log(x numeric) numeric
log(base numeric, x numeric) numeric
log10(x float8) float8
log10(x numeric) numeric
mod(y int8, x int8) int8
mod(y int4, x int4) int4
mod(y int2, x int2) int2
mod(y numeric, x numeric) numeric
div(y numeric, x numeric) numeric
degrees(x float8) float8
radians(x float8) float8
pi() float8 never-null
random() float8 never-null
random(min int4, max int4) int4
random(min int8, max int8) int8
random(min numeric, max numeric) numeric
setseed(seed float8) float8
factorial(x int8) numeric
gcd(a int8, b int8) int8
gcd(a int4, b int4) int4
gcd(a numeric, b numeric) numeric
lcm(a int8, b int8) int8
lcm(a int4, b int4) int4
lcm(a numeric, b numeric) numeric
width_bucket(operand float8, low float8, high float8, count int4) int4
width_bucket(operand numeric, low numeric, high numeric, count int4) int4

acos(x float8) float8
asin(x float8) float8
atan(x float8) float8
atan2(y float8, x float8) float8
cos(x float8) float8
sin(x float8) float8
tan(x float8) float8
cot(x float8) float8
acosd(x float8) float8
asind(x float8) float8
atand(x float8) float8
atan2d(y float8, x float8) float8
cosd(x float8) float8
sind(x float8) float8
tand(x float8) float8
cotd(x float8) float8

point(x float8, y float8) point

length(x text) int4
char_length(x text) int4
character_length(x text) int4
octet_length(x text) int4
bit_length(x text) int4
lower(x text) text
upper(x text) text
trim(x text) text
ltrim(x text) text
ltrim(x text, characters text) text
rtrim(x text) text
rtrim(x text, characters text) text
btrim(x text) text
btrim(x text, characters text) text
lpad(string text, length int4) text
lpad(string text, length int4, fill text) text
rpad(string text, length int4) text
rpad(string text, length int4, fill text) text
repeat(string text, number int4) text
replace(string text, from text, to text) text
reverse(x text) text
substr(string text, start int4) text
substr(string text, start int4, count int4) text
substring(string text, start int4) text
substring(string text, start int4, count int4) text
substring(string text, pattern text) text called-on-null
substring(string text, pattern text, escape text) text called-on-null
overlay(string text, new_value text, start int4) text
overlay(string text, new_value text, start int4, count int4) text
left(string text, n int4) text
right(string text, n int4) text

regexp_replace(string text, pattern text, replacement text) text
regexp_replace(string text, pattern text, replacement text, flags text) text
regexp_replace(string text, pattern text, replacement text, start int4, n int4?, flags text?) text
regexp_match(string text, pattern text, flags text?) text[] called-on-null
regexp_matches(string text, pattern text, flags text?) text[] returns-set
regexp_split_to_array(string text, pattern text, flags text?) text[]
regexp_split_to_table(string text, pattern text, flags text?) text returns-set
regexp_count(string text, pattern text, start int4?, flags text?) int4
regexp_like(string text, pattern text, flags text?) bool
regexp_substr(string text, pattern text, start int4?, n int4?, flags text?, subexpr int4?) text called-on-null
regexp_instr(string text, pattern text, start int4?, n int4?, endoption int4?, flags text?, subexpr int4?) int4

concat(value any) text never-null variadic=1
concat_ws(separator text, value any) text never-null variadic=2
format(formatstr text, value any) text variadic=1

initcap(x text) text
normalize(string text) text
normalize(string text, form text) text
translate(string text, from text, to text) text
ascii(x text) int4
chr(x int4) text
encode(data bytea, format text) text
decode(string text, format text) bytea
md5(x text) text
md5(x bytea) text
sha224(x bytea) bytea
sha256(x bytea) bytea
sha384(x bytea) bytea
sha512(x bytea) bytea
convert_from(bytes bytea, src_encoding text) text
convert_to(string text, dest_encoding text) bytea
to_hex(x int4) text
to_hex(x int8) text
position(substring text, string text) int4
strpos(string text, substring text) int4
starts_with(string text, prefix text) bool
split_part(string text, delimiter text, n int4) text
quote_ident(x text) text
quote_literal(x text) text
quote_nullable(x text) text called-on-null

length(x bytea) int4
length(bytes bytea, encoding text) int4
octet_length(x bytea) int4
bit_length(x bytea) int4
substr(bytes bytea, start int4, count int4?) bytea
substring(bytes bytea, start int4, count int4?) bytea
position(substring bytea, bytes bytea) int4
btrim(bytes bytea, bytesremoved bytea) bytea
ltrim(bytes bytea, bytesremoved bytea) bytea
rtrim(bytes bytea, bytesremoved bytea) bytea
overlay(bytes bytea, new_value bytea, start int4, count int4?) bytea
get_byte(bytes bytea, offset int4) int4
set_byte(bytes bytea, offset int4, new_value int4) bytea
get_bit(bytes bytea, offset int8) int4
set_bit(bytes bytea, offset int8, new_value int4) bytea
bit_count(bytes bytea) int8

now() timestamptz never-null
current_timestamp() timestamptz never-null
current_date() date never-null
current_time() timetz never-null
localtime() time never-null
localtimestamp() timestamp never-null
clock_timestamp() timestamptz never-null
statement_timestamp() timestamptz never-null
transaction_timestamp() timestamptz never-null
timeofday() text never-null
age(timestamp timestamptz, timestamp timestamptz) interval
age(timestamp timestamptz) interval
date_part(field text, source timestamptz) float8
date_part(field text, source interval) float8
date_trunc(field text, source timestamptz) timestamptz
date_trunc(field text, source interval) interval
date_trunc(field text, source timestamptz, timezone text) timestamptz
extract(field text, source timestamptz) numeric
extract(field text, source interval) numeric
timezone(zone text, timestamp timestamptz) timestamp
timezone(zone text, timestamp timestamp) timestamptz
timezone(zone interval, timestamp timestamptz) timestamp
timezone(zone interval, timestamp timestamp) timestamptz

make_date(year int4, month int4, day int4) date
make_time(hour int4, min int4, sec float8) time
make_timestamp(year int4, month int4, day int4, hour int4, min int4, sec float8) timestamp
make_timestamptz(year int4, month int4, day int4, hour int4, min int4, sec float8) timestamptz
make_timestamptz(year int4, month int4, day int4, hour int4, min int4, sec float8, timezone text) timestamptz
make_interval(years int4?, months int4?, weeks int4?, days int4?, hours int4?, mins int4?, secs float8?) interval

to_timestamp(epoch float8) timestamptz
to_timestamp(text text, format text) timestamptz
to_date(text text, format text) date
to_char(timestamp timestamptz, format text) text
to_char(interval interval, format text) text
to_char(value numeric, format text) text
isfinite(value date) bool
isfinite(value timestamptz) bool
isfinite(value interval) bool
justify_days(value interval) interval
justify_hours(value interval) interval
justify_interval(value interval) interval
date_bin(stride interval, source timestamp, origin timestamp) timestamp
date_bin(stride interval, source timestamptz, origin timestamptz) timestamptz
date(value timestamp) date
date(value timestamptz) date
date_add(base timestamptz, increment interval, zone text?) timestamptz
date_subtract(base timestamptz, decrement interval, zone text?) timestamptz

json_build_object(key any, value any) json never-null variadic=0
json_build_array(value any) json never-null variadic=0
jsonb_build_object(key any, value any) jsonb never-null variadic=0
jsonb_build_array(value any) jsonb never-null variadic=0

to_json(value any) json
to_jsonb(value any) jsonb
row_to_json(record any) json
array_to_json(array any) json
array_to_json(array any, pretty bool) json
json_array_length(json json) int4
jsonb_array_length(json jsonb) int4
json_typeof(json json) text
jsonb_typeof(json jsonb) text

json_extract_path(from_json json, path_elem text) json called-on-null variadic=2
json_extract_path_text(from_json json, path_elem text) text called-on-null variadic=2
jsonb_extract_path(from_json jsonb, path_elem text) jsonb called-on-null variadic=2
jsonb_extract_path_text(from_json jsonb, path_elem text) text called-on-null variadic=2
jsonb_path_exists(target jsonb, path jsonpath, vars jsonb?, silent bool?) bool called-on-null
jsonb_path_match(target jsonb, path jsonpath, vars jsonb?, silent bool?) bool called-on-null
jsonb_path_query(target jsonb, path jsonpath, vars jsonb?, silent bool?) jsonb returns-set
jsonb_path_query_first(target jsonb, path jsonpath, vars jsonb?, silent bool?) jsonb called-on-null
jsonb_path_query_array(target jsonb, path jsonpath, vars jsonb?, silent bool?) jsonb

jsonb_set(target jsonb, path text[], new_value jsonb, create_if_missing bool?) jsonb
jsonb_insert(target jsonb, path text[], new_value jsonb, insert_after bool?) jsonb
jsonb_set_lax(target jsonb, path text[], new_value jsonb, create_if_missing bool?, null_value_treatment text?) jsonb called-on-null
jsonb_strip_nulls(json jsonb) jsonb
json_strip_nulls(json json) json
json_object(keys_values text[]) json
json_object(keys text[], values text[]) json
jsonb_object(keys_values text[]) jsonb
jsonb_object(keys text[], values text[]) jsonb
jsonb_pretty(json jsonb) text

array_append(array any, element any) any
array_cat(array1 any, array2 any) any
array_dims(array any) text
array_fill(value any, dimensions any) any
array_length(array any, dimension int4) int4 called-on-null
array_lower(array any, dimension int4) int4 called-on-null
array_upper(array any, dimension int4) int4 called-on-null
array_ndims(array any) int4
array_position(array any, element any) int4 called-on-null
array_position(array any, element any, start int4) int4 called-on-null
array_positions(array any, element any) any
array_prepend(element any, array any) any
array_remove(array any, element any) any
array_replace(array any, from any, to any) any
array_to_string(array any, delimiter text) text
array_to_string(array any, delimiter text, null_string text) text
string_to_array(string text, delimiter text) any
string_to_array(string text, delimiter text, null_string text) any
cardinality(array any) int4
trim_array(array any, n int4) any return-type-from=1

jsonb_array_elements(from_json jsonb) jsonb returns-set
jsonb_array_elements_text(from_json jsonb) text called-on-null returns-set
json_array_elements(from_json json) json returns-set
json_array_elements_text(from_json json) text called-on-null returns-set
jsonb_object_keys(from_json jsonb) text returns-set
json_object_keys(from_json json) text returns-set
jsonb_each(from_json jsonb) any returns-set
jsonb_each_text(from_json jsonb) any returns-set
json_each(from_json json) any returns-set
json_each_text(from_json json) any returns-set
generate_series(start any, stop any, step any?) any returns-set

int4range(lower int4, upper int4, bounds text?) int4range never-null
int8range(lower int8, upper int8, bounds text?) int8range never-null
numrange(lower numeric, upper numeric, bounds text?) numrange never-null
tsrange(lower timestamp, upper timestamp, bounds text?) tsrange never-null
tstzrange(lower timestamptz, upper timestamptz, bounds text?) tstzrange never-null
daterange(lower date, upper date, bounds text?) daterange never-null
isempty(range int4range) bool
lower_inc(range int4range) bool
upper_inc(range int4range) bool
lower_inf(range int4range) bool
upper_inf(range int4range) bool
range_merge(range1 int4range, range2 int4range) any return-type-from=1
range_agg(range int4range) any called-on-null aggregate

enum_first(value any) any never-null return-type-from=1
enum_last(value any) any never-null return-type-from=1
enum_range(value any, upper any?) any never-null

sum(x any) any called-on-null aggregate
avg(x any) any called-on-null aggregate
min(x any) any called-on-null aggregate
max(x any) any called-on-null aggregate
unnest(x any) any called-on-null variadic=1 returns-set
json_populate_record(base any, from_json json) any called-on-null
json_populate_recordset(base any, from_json json) any called-on-null returns-set
json_to_record(json any) any called-on-null
json_to_recordset(json any) any called-on-null returns-set
jsonb_populate_record(base any, from_json jsonb) any called-on-null
jsonb_populate_recordset(base any, from_json jsonb) any called-on-null returns-set
jsonb_to_record(json any) any called-on-null
jsonb_to_recordset(json any) any called-on-null returns-set

grouping(expression any) int4 never-null variadic=1

count() int8 never-null aggregate
count(x any) int8 never-null aggregate

bool_and(x bool) bool called-on-null aggregate
bool_or(x bool) bool called-on-null aggregate
every(x bool) bool called-on-null aggregate
bit_and(x any) any called-on-null aggregate
bit_or(x any) any called-on-null aggregate
bit_xor(x any) any called-on-null aggregate

string_agg(expression text, delimiter text) text called-on-null aggregate
string_agg(expression bytea, delimiter bytea) bytea called-on-null aggregate
array_agg(expression any) any called-on-null aggregate

json_agg(expression any) json called-on-null aggregate
jsonb_agg(expression any) jsonb called-on-null aggregate
json_object_agg(key any, value any) json called-on-null aggregate
jsonb_object_agg(key any, value any) jsonb called-on-null aggregate

percentile_cont(fraction any, value any) any called-on-null aggregate
percentile_disc(fraction any, value any) any called-on-null aggregate
mode(value any) any called-on-null aggregate

any_value(value any) any called-on-null aggregate return-type-from=1

corr(y float8, x float8) float8 called-on-null aggregate
covar_pop(y float8, x float8) float8 called-on-null aggregate
covar_samp(y float8, x float8) float8 called-on-null aggregate
regr_avgx(y float8, x float8) float8 called-on-null aggregate
regr_avgy(y float8, x float8) float8 called-on-null aggregate
regr_count(y float8, x float8) int8 never-null aggregate
regr_intercept(y float8, x float8) float8 called-on-null aggregate
regr_r2(y float8, x float8) float8 called-on-null aggregate
regr_slope(y float8, x float8) float8 called-on-null aggregate
regr_sxx(y float8, x float8) float8 called-on-null aggregate
regr_sxy(y float8, x float8) float8 called-on-null aggregate
regr_syy(y float8, x float8) float8 called-on-null aggregate
stddev(x any) numeric called-on-null aggregate
stddev_pop(x any) numeric called-on-null aggregate
stddev_samp(x any) numeric called-on-null aggregate
variance(x any) numeric called-on-null aggregate
var_pop(x any) numeric called-on-null aggregate
var_samp(x any) numeric called-on-null aggregate

row_number() int8 never-null
rank() int8 never-null
dense_rank() int8 never-null
ntile(n int4) int4 never-null
cume_dist() float8 never-null
percent_rank() float8 never-null
lag(expression any, offset int4, default any) any called-on-null min-arguments=1
lead(expression any, offset int4, default any) any called-on-null min-arguments=1
first_value(expression any) any called-on-null
last_value(expression any) any called-on-null
nth_value(expression any, n int4) any called-on-null

nullif(value1 any, value2 any) any called-on-null
greatest(value any) any called-on-null variadic=1
least(value any) any called-on-null variadic=1
num_nulls(value any) int4 never-null variadic=1
num_nonnulls(value any) int4 never-null variadic=1

to_char(value int4, format text) text
to_number(text text, format text) numeric

current_database() text never-null
current_schema() text never-null
current_user() text never-null
session_user() text never-null
current_role() text never-null
current_catalog() text never-null
version() text never-null
pg_typeof(value any) text
pg_column_size(value any) int4
pg_table_size(table text) int8
pg_total_relation_size(table text) int8
obj_description(oid int4, catalog text) text called-on-null
obj_description(oid int4) text called-on-null
obj_description(object regclass, catalog text) text called-on-null
col_description(table int4, column int4) text called-on-null
col_description(table regclass, column int4) text called-on-null
shobj_description(object int4, catalog text) text called-on-null
gen_random_uuid() uuid never-null
pg_size_pretty(size int8) text
pg_size_pretty(size numeric) text
pg_size_bytes(size text) int8
pg_database_size(database text) int8
pg_database_size(database oid) int8
pg_relation_size(relation text, fork text?) int8
pg_indexes_size(relation text) int8

txid_current() int8 never-null
txid_current_if_assigned() int8 called-on-null
pg_current_xact_id() xid8 never-null
pg_current_xact_id_if_assigned() xid8 called-on-null

current_setting(setting_name text) text
current_setting(setting_name text, missing_ok bool) text called-on-null
set_config(setting_name text, new_value text, is_local bool) text never-null

pg_advisory_lock(key int8) void modifies-data
pg_advisory_lock(key1 int4, key2 int4) void modifies-data
pg_advisory_lock_shared(key int8) void modifies-data
pg_advisory_lock_shared(key1 int4, key2 int4) void modifies-data
pg_advisory_xact_lock(key int8) void modifies-data
pg_advisory_xact_lock(key1 int4, key2 int4) void modifies-data
pg_advisory_xact_lock_shared(key int8) void modifies-data
pg_advisory_xact_lock_shared(key1 int4, key2 int4) void modifies-data
pg_advisory_unlock(key int8) bool modifies-data
pg_advisory_unlock(key1 int4, key2 int4) bool modifies-data
pg_advisory_unlock_shared(key int8) bool modifies-data
pg_advisory_unlock_shared(key1 int4, key2 int4) bool modifies-data
pg_advisory_unlock_all() void never-null modifies-data
pg_try_advisory_lock(key int8) bool modifies-data
pg_try_advisory_lock(key1 int4, key2 int4) bool modifies-data
pg_try_advisory_lock_shared(key int8) bool modifies-data
pg_try_advisory_lock_shared(key1 int4, key2 int4) bool modifies-data
pg_try_advisory_xact_lock(key int8) bool modifies-data
pg_try_advisory_xact_lock(key1 int4, key2 int4) bool modifies-data
pg_try_advisory_xact_lock_shared(key int8) bool modifies-data
pg_try_advisory_xact_lock_shared(key1 int4, key2 int4) bool modifies-data
pg_notify(channel text, payload text) void never-null modifies-data
pg_sleep(seconds float8) void

nextval(sequence text) int8 never-null modifies-data
setval(sequence text, value int8) int8 never-null modifies-data
setval(sequence text, value int8, is_called bool) int8 never-null modifies-data
currval(sequence text) int8 never-null
lastval() int8 never-null

to_tsvector(x text) tsvector
to_tsvector(config text, x text) tsvector
to_tsvector(document json) tsvector
to_tsvector(document jsonb) tsvector
to_tsvector(config text, document json) tsvector
to_tsvector(config text, document jsonb) tsvector
to_tsquery(x text) tsquery
to_tsquery(config text, x text) tsquery
plainto_tsquery(x text) tsquery
plainto_tsquery(config text, x text) tsquery
phraseto_tsquery(x text) tsquery
phraseto_tsquery(config text, x text) tsquery
websearch_to_tsquery(x text) tsquery
websearch_to_tsquery(config text, x text) tsquery
ts_rank(vector tsvector, query tsquery) float4
ts_rank(vector tsvector, query tsquery, normalisation int4) float4
ts_rank_cd(vector tsvector, query tsquery) float4
ts_rank_cd(vector tsvector, query tsquery, normalisation int4) float4
ts_rank(weights float4[], vector tsvector, query tsquery, normalisation int4?) float4
ts_rank_cd(weights float4[], vector tsvector, query tsquery, normalisation int4?) float4
ts_headline(document text, query tsquery) text
ts_headline(config text, document text, query tsquery) text
ts_headline(config text, document text, query tsquery, options text) text
ts_headline(document text, query tsquery, options text) text
setweight(vector tsvector, weight text) tsvector
strip(vector tsvector) tsvector
length(vector tsvector) int4
numnode(query tsquery) int4
querytree(query tsquery) text
similarity(a text, b text) float4
word_similarity(a text, b text) float4
strict_word_similarity(a text, b text) float4
