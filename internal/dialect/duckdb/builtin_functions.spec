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

abs(x any) any return-type-from=1
ceil(x any) any
ceiling(x any) any
floor(x any) any
round(x any) any return-type-from=1
round(x any, s integer) any return-type-from=1
trunc(x any) any return-type-from=1
trunc(x any, s integer) any return-type-from=1
sign(x any) tinyint
sqrt(x double) double
cbrt(x double) double
power(a double, b double) double
pow(a double, b double) double
exp(x double) double
ln(x double) double
log(x double) double
log(base double, x double) double
log10(x double) double
log2(x double) double
mod(y any, x any) any
degrees(x double) double
radians(x double) double
pi() double never-null
random() double never-null
setseed(seed double) double
factorial(x bigint) numeric
gcd(a bigint, b bigint) bigint
lcm(a bigint, b bigint) bigint
width_bucket(operand numeric, low numeric, high numeric, count integer) integer
even(x numeric) numeric
isnan(x double) boolean
isinf(x double) boolean
isinf(x timestamp) boolean
isinf(x timestamptz) boolean
isfinite(x double) boolean
isfinite(x timestamp) boolean
isfinite(x timestamptz) boolean
bit_count(x any) bigint
fmod(x double, y double) double
fdiv(x double, y double) double
round_even(x numeric, n integer) double
roundbankers(x numeric, n integer) double
greatest_common_divisor(a bigint, b bigint) bigint
least_common_multiple(a bigint, b bigint) bigint
xor(left any, right any) any return-type-from=1
signbit(x double) boolean
nextafter(x double, y double) double
gamma(x double) double
lgamma(x double) double

acos(x double) double
asin(x double) double
atan(x double) double
atan2(y double, x double) double
cos(x double) double
sin(x double) double
tan(x double) double
cot(x double) double
acosh(x double) double
asinh(x double) double
atanh(x double) double
cosh(x double) double
sinh(x double) double
tanh(x double) double

length(x varchar) bigint
length(list any) bigint
len(list any) bigint
strlen(x varchar) bigint
char_length(x varchar) bigint
octet_length(x any) bigint
bit_length(x varchar) bigint
lower(x varchar) varchar
upper(x varchar) varchar
trim(x varchar) varchar
trim(x varchar, characters varchar) varchar
ltrim(x varchar) varchar
ltrim(x varchar, characters varchar) varchar
rtrim(x varchar) varchar
rtrim(x varchar, characters varchar) varchar
lpad(string varchar, length integer) varchar
lpad(string varchar, length integer, fill varchar) varchar
rpad(string varchar, length integer) varchar
rpad(string varchar, length integer, fill varchar) varchar
repeat(string varchar, number integer) varchar
replace(string varchar, from varchar, to varchar) varchar
reverse(x varchar) varchar
substr(string varchar, start integer) varchar
substr(string varchar, start integer, count integer) varchar
substring(string varchar, start integer) varchar
substring(string varchar, start integer, count integer) varchar
left(string varchar, n integer) varchar
right(string varchar, n integer) varchar

concat(value any) varchar never-null variadic=1
concat_ws(separator varchar, value any) varchar never-null variadic=2
format(formatstr varchar, value any) varchar variadic=1
printf(formatstr varchar, value any) varchar variadic=1

initcap(x varchar) varchar
translate(string varchar, from varchar, to varchar) varchar
ascii(x varchar) integer
chr(x integer) varchar
encode(data blob, format varchar) varchar
decode(string varchar, format varchar) blob
md5(x varchar) varchar
md5(x blob) varchar
position(substring varchar, string varchar) bigint
strpos(string varchar, substring varchar) bigint
instr(string varchar, substring varchar) bigint
starts_with(string varchar, prefix varchar) boolean
prefix(string varchar, search_string varchar) boolean
suffix(string varchar, search_string varchar) boolean
ends_with(string varchar, suffix varchar) boolean
contains(string varchar, search_string varchar) boolean
contains(list any[], element any) boolean
contains(map any, key any) boolean
split_part(string varchar, delimiter varchar, n integer) varchar
regexp_extract(string varchar, pattern varchar) varchar
regexp_extract(string varchar, pattern varchar, group integer, options varchar?) varchar
regexp_extract(string varchar, pattern varchar, names varchar[], options varchar?) any
regexp_replace(string varchar, pattern varchar, replacement varchar) varchar
regexp_replace(string varchar, pattern varchar, replacement varchar, options varchar) varchar
regexp_full_match(string varchar, pattern varchar) boolean
regexp_matches(string varchar, pattern varchar) boolean
regexp_matches(string varchar, pattern varchar, options varchar) boolean

string_split(string varchar, separator varchar) varchar[]
str_split(string varchar, separator varchar) varchar[]
split(string varchar, separator varchar) varchar[]
string_split_regex(string varchar, pattern varchar, options varchar?) varchar[]
str_split_regex(string varchar, pattern varchar, options varchar?) varchar[]
regexp_split_to_array(string varchar, pattern varchar, options varchar?) varchar[]
regexp_extract_all(string varchar, pattern varchar, group any?, options varchar?) varchar[]
regexp_escape(string varchar) varchar
lcase(x varchar) varchar
ucase(x varchar) varchar
character_length(x varchar) bigint
strip_accents(x varchar) varchar
nfc_normalize(x varchar) varchar
url_encode(x varchar) varchar
url_decode(x varchar) varchar
ord(x varchar) integer
unicode(x varchar) integer
sha1(value any) varchar
sha256(value any) varchar
hex(value any) varchar
to_hex(value any) varchar
unhex(string varchar) blob
from_hex(string varchar) blob
base64(data blob) varchar
to_base64(data blob) varchar
from_base64(string varchar) blob
format_bytes(bytes bigint) varchar

levenshtein(left varchar, right varchar) bigint
editdist3(left varchar, right varchar) bigint
damerau_levenshtein(left varchar, right varchar) bigint
hamming(left varchar, right varchar) bigint
mismatches(left varchar, right varchar) bigint
jaccard(left varchar, right varchar) double
jaro_similarity(left varchar, right varchar, score_cutoff double?) double
jaro_winkler_similarity(left varchar, right varchar, score_cutoff double?) double

now() timestamptz never-null
current_timestamp() timestamptz never-null
current_date() date never-null
today() date never-null
current_time() time never-null
age(timestamp timestamptz, timestamp timestamptz) interval
age(timestamp timestamptz) interval
date_part(field varchar, source timestamptz) double
date_part(field varchar, source interval) double
date_trunc(field varchar, source timestamptz) timestamptz
date_trunc(field varchar, source interval) interval
extract(field varchar, source timestamptz) numeric
extract(field varchar, source interval) numeric
make_date(year integer, month integer, day integer) date
make_time(hour integer, min integer, sec double) time
make_timestamp(year integer, month integer, day integer, hour integer, min integer, sec double) timestamp
make_timestamptz(year integer, month integer, day integer, hour integer, min integer, sec double) timestamptz
make_timestamptz(year integer, month integer, day integer, hour integer, min integer, sec double, timezone varchar) timestamptz
to_timestamp(epoch double) timestamptz
to_timestamp(text varchar, format varchar) timestamptz
to_date(text varchar, format varchar) date
to_char(timestamp timestamptz, format varchar) varchar
to_char(interval interval, format varchar) varchar
to_char(value numeric, format varchar) varchar
strftime(timestamp timestamptz, format varchar) varchar
strftime(timestamp timestamp, format varchar) varchar
strftime(date date, format varchar) varchar
strftime(format varchar, timestamp timestamptz) varchar
strftime(format varchar, timestamp timestamp) varchar
strftime(format varchar, date date) varchar
strptime(string varchar, format varchar) timestamp
epoch_ms(value bigint) timestamp
epoch_us(value bigint) timestamp
epoch_ns(value bigint) timestamp
last_day(value date) date
dayname(value date) varchar
monthname(value date) varchar

year(source timestamptz) bigint
month(source timestamptz) bigint
day(source timestamptz) bigint
hour(source timestamptz) bigint
minute(source timestamptz) bigint
second(source timestamptz) bigint
millisecond(source timestamptz) bigint
microsecond(source timestamptz) bigint
quarter(source timestamptz) bigint
week(source timestamptz) bigint
weekday(source timestamptz) bigint
weekofyear(source timestamptz) bigint
dayofweek(source timestamptz) bigint
dayofmonth(source timestamptz) bigint
dayofyear(source timestamptz) bigint
isodow(source timestamptz) bigint
isoyear(source timestamptz) bigint
yearweek(source timestamptz) bigint
era(source timestamptz) bigint
century(source timestamptz) bigint
decade(source timestamptz) bigint
millennium(source timestamptz) bigint
julian(source timestamptz) double
epoch(source timestamptz) double
epoch_ms(source timestamptz) bigint
epoch_us(source timestamptz) bigint
epoch_ns(source timestamptz) bigint
datepart(field varchar, source timestamptz) bigint
datetrunc(field varchar, source timestamptz) timestamptz

date_diff(part varchar, start timestamptz, end timestamptz) bigint
datediff(part varchar, start timestamptz, end timestamptz) bigint
date_sub(part varchar, start timestamptz, end timestamptz) bigint
datesub(part varchar, start timestamptz, end timestamptz) bigint
date_add(value timestamptz, interval interval) timestamp
time_bucket(width interval, value timestamptz, origin any?) timestamptz return-type-from=2
try_strptime(string varchar, format varchar) timestamp called-on-null
timezone(zone varchar, value timestamptz) timestamptz
transaction_timestamp() timestamptz never-null
get_current_timestamp() timestamptz never-null
current_localtimestamp() timestamp never-null
current_localtime() time never-null

to_years(value integer) interval
to_months(value integer) interval
to_quarters(value integer) interval
to_weeks(value integer) interval
to_days(value integer) interval
to_decades(value integer) interval
to_centuries(value integer) interval
to_millennia(value integer) interval
to_hours(value bigint) interval
to_minutes(value bigint) interval
to_seconds(value double) interval
to_milliseconds(value double) interval
to_microseconds(value bigint) interval

to_json(value any) json
row_to_json(record struct) json
json_array_length(json json) ubigint
json_array_length(json json, path varchar) ubigint called-on-null
json_array_length(json json, paths varchar[]) ubigint[]
json_type(json json) varchar
json_type(json json, path varchar) varchar called-on-null
json_type(json json, paths varchar[]) varchar[]
json_valid(json varchar) boolean
json_extract(json json, path varchar) json called-on-null
json_extract(json json, index bigint) json called-on-null
json_extract(json json, paths varchar[]) json[]
json_extract_string(json json, path varchar) varchar called-on-null
json_extract_string(json json, index bigint) varchar called-on-null
json_extract_string(json json, paths varchar[]) varchar[]
json_object(key any, value any) json never-null variadic=0
json_array(value any) json never-null variadic=0
json_merge_patch(target json, patch json) json
json_keys(json json) any
json_keys(json json, path varchar) varchar[] called-on-null
json_keys(json json, paths varchar[]) varchar[][]
json_exists(json json, path varchar) boolean
json_value(json json, path varchar) varchar called-on-null
json_value(json json, index bigint) varchar called-on-null
json_value(json json, paths varchar[]) varchar[]
json_contains(json json, other json) boolean
json_structure(json json) json
json_pretty(json json) varchar
json_extract_path(json json, path varchar) json called-on-null
json_extract_path(json json, paths varchar[]) json[]
json_extract_path_text(json json, path varchar) varchar called-on-null
json_extract_path_text(json json, paths varchar[]) varchar[]
json(value varchar) json

list_value(value any) any never-null variadic=0

list_sort(list any) any return-type-from=1
list_sort(list any, sort_order varchar) any return-type-from=1
list_sort(list any, sort_order varchar, null_order varchar) any return-type-from=1
array_sort(list any) any return-type-from=1
array_sort(list any, sort_order varchar) any return-type-from=1
array_sort(list any, sort_order varchar, null_order varchar) any return-type-from=1
list_reverse_sort(list any) any return-type-from=1
list_reverse_sort(list any, null_order varchar) any return-type-from=1
array_reverse_sort(list any) any return-type-from=1
array_reverse_sort(list any, null_order varchar) any return-type-from=1
list_distinct(list any) any return-type-from=1
array_distinct(list any) any return-type-from=1

list_unique(list any) ubigint
list_contains(list any, element any) boolean
list_aggregate(list any, name varchar) any
list_aggregate(list any, name varchar, argument any) any variadic=3
array_length(array any) bigint
array_append(array any, element any) any
array_cat(array1 any, array2 any) any
array_position(array any, element any) integer called-on-null
array_to_string(array any, delimiter varchar) varchar
string_to_array(string varchar, delimiter varchar) any
cardinality(array any) ubigint
flatten(list any) any
unnest(list any) any

list_has(list any, element any) boolean
array_has(list any, element any) boolean
array_contains(list any, element any) boolean
list_has_any(list any, other any) boolean
array_has_any(list any, other any) boolean
list_has_all(list any, other any) boolean
array_has_all(list any, other any) boolean
list_position(list any, element any) integer called-on-null
list_indexof(list any, element any) integer called-on-null
array_indexof(list any, element any) integer called-on-null
array_unique(list any) ubigint
list_grade_up(list any, sort_order varchar?, null_order varchar?) bigint[]
array_grade_up(list any, sort_order varchar?, null_order varchar?) bigint[]
list_cosine_similarity(left any, right any) double
array_cosine_similarity(left any, right any) double
list_distance(left any, right any) double
array_distance(left any, right any) double
list_inner_product(left any, right any) double
array_inner_product(left any, right any) double
list_dot_product(left any, right any) double
array_dot_product(left any, right any) double

list_concat(list any, other any) any variadic=1 return-type-from=1
list_cat(list any, other any) any variadic=1 return-type-from=1
array_concat(list any, other any) any variadic=1 return-type-from=1
list_slice(list any, begin any, end any, step bigint?) any return-type-from=1
array_slice(list any, begin any, end any, step bigint?) any return-type-from=1
list_intersect(list any, other any) any return-type-from=1
array_intersect(list any, other any) any return-type-from=1
list_resize(list any, size any, value any?) any return-type-from=1
array_resize(list any, size any, value any?) any return-type-from=1
list_reverse(list any) any return-type-from=1
array_reverse(list any) any return-type-from=1
list_append(list any, element any) any return-type-from=1
array_push_back(list any, element any) any return-type-from=1
list_prepend(element any, list any) any return-type-from=2
array_push_front(list any, element any) any return-type-from=1
array_pop_back(list any) any return-type-from=1
array_pop_front(list any) any return-type-from=1
list_select(list any, indexes any) any return-type-from=1
array_select(list any, indexes any) any return-type-from=1
list_where(list any, mask any) any return-type-from=1
array_where(list any, mask any) any return-type-from=1

list_avg(list any) double called-on-null
list_count(list any) bigint
list_product(list any) double called-on-null
list_string_agg(list any) varchar called-on-null
list_bool_and(list any) boolean called-on-null
list_bool_or(list any) boolean called-on-null
list_stddev_samp(list any) double called-on-null
list_stddev_pop(list any) double called-on-null
list_var_samp(list any) double called-on-null
list_var_pop(list any) double called-on-null
list_entropy(list any) double
list_kurtosis(list any) double called-on-null
list_skewness(list any) double called-on-null
list_sem(list any) double called-on-null
list_approx_count_distinct(list any) bigint
list_median(list any) any called-on-null

grouping(expression any) bigint never-null variadic=1
grouping_id(expression any) bigint never-null variadic=1

count() bigint never-null aggregate
count(x any) bigint never-null aggregate
count_star() bigint never-null aggregate
count_if(condition boolean) hugeint called-on-null aggregate
countif(condition boolean) hugeint called-on-null aggregate
approx_count_distinct(x any) bigint called-on-null aggregate

any_value(x any) any called-on-null aggregate return-type-from=1
arbitrary(x any) any called-on-null aggregate return-type-from=1
first(x any) any called-on-null aggregate return-type-from=1
last(x any) any called-on-null aggregate return-type-from=1
arg_min(arg any, key any) any called-on-null aggregate return-type-from=1
arg_min(arg any, key any, count bigint) any called-on-null aggregate
argmin(arg any, key any) any called-on-null aggregate return-type-from=1
argmin(arg any, key any, count bigint) any called-on-null aggregate
min_by(arg any, key any) any called-on-null aggregate return-type-from=1
min_by(arg any, key any, count bigint) any called-on-null aggregate
arg_min_null(arg any, key any) any called-on-null aggregate return-type-from=1
arg_max(arg any, key any) any called-on-null aggregate return-type-from=1
arg_max(arg any, key any, count bigint) any called-on-null aggregate
argmax(arg any, key any) any called-on-null aggregate return-type-from=1
argmax(arg any, key any, count bigint) any called-on-null aggregate
max_by(arg any, key any) any called-on-null aggregate return-type-from=1
max_by(arg any, key any, count bigint) any called-on-null aggregate
arg_max_null(arg any, key any) any called-on-null aggregate return-type-from=1

bool_and(x boolean) boolean called-on-null aggregate
bool_or(x boolean) boolean called-on-null aggregate
every(x boolean) boolean called-on-null aggregate
bit_and(x any) any called-on-null aggregate
bit_or(x any) any called-on-null aggregate
bit_xor(x any) any called-on-null aggregate

string_agg(expression any, delimiter varchar?) varchar called-on-null aggregate
group_concat(expression any, delimiter varchar?) varchar called-on-null aggregate
listagg(expression any, delimiter varchar?) varchar called-on-null aggregate
list(expression any) any called-on-null aggregate
array_agg(expression any) any called-on-null aggregate

json_group_array(expression any) json called-on-null aggregate
json_group_object(key any, value any) json called-on-null aggregate

percentile_cont(fraction double) double called-on-null aggregate
percentile_disc(fraction double) any called-on-null aggregate
mode() any called-on-null aggregate
mode(x any) any called-on-null aggregate return-type-from=1
median(x any) any called-on-null aggregate
quantile_cont(x any, fraction double) any called-on-null aggregate
quantile_cont(x any, fractions double[]) any called-on-null aggregate
quantile_disc(x any, fraction double) any called-on-null aggregate
quantile_disc(x any, fractions double[]) any called-on-null aggregate

stddev(x any) numeric called-on-null aggregate
stddev_pop(x any) numeric called-on-null aggregate
stddev_samp(x any) numeric called-on-null aggregate
variance(x any) numeric called-on-null aggregate
var_pop(x any) numeric called-on-null aggregate
var_samp(x any) numeric called-on-null aggregate

product(x double) double called-on-null aggregate
fsum(x double) double called-on-null aggregate
sumkahan(x double) double called-on-null aggregate
kahan_sum(x double) double called-on-null aggregate
favg(x double) double called-on-null aggregate
kurtosis(x double) double called-on-null aggregate
kurtosis_pop(x double) double called-on-null aggregate
skewness(x double) double called-on-null aggregate
sem(x double) double called-on-null aggregate
entropy(x any) double called-on-null aggregate
geomean(x double) double called-on-null aggregate
geometric_mean(x double) double called-on-null aggregate
weighted_avg(value double, weight double) double called-on-null aggregate
wavg(value double, weight double) double called-on-null aggregate
corr(y double, x double) double called-on-null aggregate
covar_pop(y double, x double) double called-on-null aggregate
covar_samp(y double, x double) double called-on-null aggregate
regr_slope(y double, x double) double called-on-null aggregate
regr_intercept(y double, x double) double called-on-null aggregate
regr_r2(y double, x double) double called-on-null aggregate
regr_avgx(y double, x double) double called-on-null aggregate
regr_avgy(y double, x double) double called-on-null aggregate
regr_sxx(y double, x double) double called-on-null aggregate
regr_syy(y double, x double) double called-on-null aggregate
regr_sxy(y double, x double) double called-on-null aggregate
regr_count(y double, x double) uinteger called-on-null aggregate

mean(x any) any called-on-null aggregate
approx_quantile(x any, position any) any called-on-null aggregate
reservoir_quantile(x any, position any, sample_size integer?) any called-on-null aggregate
quantile(x any, position any) any called-on-null aggregate
histogram(x any) any called-on-null aggregate
approx_top_k(x any, count bigint) any called-on-null aggregate

row_number() bigint never-null
rank() bigint never-null
dense_rank() bigint never-null
ntile(n integer) bigint never-null
cume_dist() double never-null
percent_rank() double never-null
lag(expression any, offset integer, default any) any called-on-null min-arguments=1
lead(expression any, offset integer, default any) any called-on-null min-arguments=1
first_value(expression any) any called-on-null
last_value(expression any) any called-on-null
nth_value(expression any, n integer) any called-on-null

nullif(value1 any, value2 any) any called-on-null
greatest(value any) any called-on-null variadic=1
least(value any) any called-on-null variadic=1
if(condition boolean, then any, otherwise any) any called-on-null return-type-from=2
ifnull(expression any, alternative any) any called-on-null
try(expression any) any called-on-null return-type-from=1

to_char(value integer, format varchar) varchar
to_number(text varchar, format varchar) numeric

current_database() varchar never-null
current_schema() varchar never-null
current_user() varchar never-null
version() varchar never-null
uuid() uuid never-null
gen_random_uuid() uuid never-null
uuidv4() uuid never-null
uuidv7() uuid never-null
current_catalog() varchar never-null
current_setting(name varchar) any
pg_typeof(value any) varchar never-null

columns(selector any) any called-on-null variadic=0
typeof(value any) varchar never-null
hash(value any) bigint
union_tag(union any) varchar
union_extract(union any, tag varchar) any called-on-null
union_value(value any) any called-on-null variadic=0
nextval(sequence_name varchar) bigint never-null modifies-data
currval(sequence_name varchar) bigint

generate_series(start bigint, stop bigint, step bigint) bigint[] min-arguments=1
generate_series(start timestamp, stop timestamp, step interval) timestamp[]
generate_series(start timestamptz, stop timestamptz, step interval) timestamptz[]
range(start bigint, stop bigint, step bigint) bigint[] min-arguments=1
range(start timestamp, stop timestamp, step interval) timestamp[]
range(start timestamptz, stop timestamptz, step interval) timestamptz[]
