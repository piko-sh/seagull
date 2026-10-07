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

time_bucket(bucket_width interval, ts timestamptz) timestamptz
time_bucket(bucket_width interval, ts timestamp) timestamp
time_bucket(bucket_width interval, ts date) date
time_bucket(bucket_width interval, ts timestamptz, offset interval) timestamptz
time_bucket(bucket_width interval, ts timestamptz, origin timestamptz) timestamptz
time_bucket(bucket_width interval, ts timestamptz, timezone text) timestamptz

time_bucket(bucket_width int2, ts int2) int2
time_bucket(bucket_width int2, ts int2, offset int2) int2
time_bucket(bucket_width int2, ts int2, origin int2) int2
time_bucket(bucket_width int4, ts int4) int4
time_bucket(bucket_width int4, ts int4, offset int4) int4
time_bucket(bucket_width int4, ts int4, origin int4) int4
time_bucket(bucket_width int8, ts int8) int8
time_bucket(bucket_width int8, ts int8, offset int8) int8
time_bucket(bucket_width int8, ts int8, origin int8) int8

time_bucket(bucket_width interval, ts uuid) timestamptz
time_bucket(bucket_width interval, ts uuid, timezone text) timestamptz
time_bucket(bucket_width interval, ts uuid, origin timestamptz) timestamptz
time_bucket(bucket_width interval, ts uuid, offset interval) timestamptz

time_bucket_gapfill(bucket_width interval, ts timestamptz) timestamptz
time_bucket_gapfill(bucket_width interval, ts timestamptz, start timestamptz) timestamptz
time_bucket_gapfill(bucket_width interval, ts timestamptz, start timestamptz, finish timestamptz) timestamptz
time_bucket_gapfill(bucket_width interval, ts timestamptz, timezone text, start timestamptz, finish timestamptz) timestamptz
locf(value any) any called-on-null
locf(value any, prev any) any called-on-null
locf(value any, prev any, treat_null_as_missing bool) any called-on-null
interpolate(value any) any called-on-null
interpolate(value any, prev any) any called-on-null
interpolate(value any, prev any, next any) any called-on-null
heartbeat_agg(ts timestamptz, start timestamptz, agg_interval interval, heartbeat_interval interval) heartbeat called-on-null aggregate

first(value any, time timestamptz) any called-on-null aggregate return-type-from=1
last(value any, time timestamptz) any called-on-null aggregate return-type-from=1
first(value numeric, time timestamptz) numeric called-on-null aggregate
last(value numeric, time timestamptz) numeric called-on-null aggregate
first(value text, time timestamptz) text called-on-null aggregate
last(value text, time timestamptz) text called-on-null aggregate

histogram(value float8, min float8, max float8, nbuckets int4) int4[] called-on-null aggregate
stats_agg(y float8) statssummary1d called-on-null aggregate
stats_agg(y float8, x float8) statssummary2d called-on-null aggregate
counter_agg(ts timestamptz, value float8) counter_summary called-on-null aggregate
gauge_agg(ts timestamptz, value float8) gauge_summary called-on-null aggregate

average(summary statssummary1d) float8
mean(summary statssummary1d) float8
sum(summary statssummary1d) float8
num_vals(summary statssummary1d) int8
num_vals(summary statssummary2d) int8
stddev(summary statssummary1d) float8
variance(summary statssummary1d) float8
skewness(summary statssummary1d) float8
kurtosis(summary statssummary1d) float8
corr(summary statssummary2d) float8
slope(summary statssummary2d) float8
intercept(summary statssummary2d) float8
x_intercept(summary statssummary2d) float8
determination_coeff(summary statssummary2d) float8

average_x(summary statssummary2d) float8
average_y(summary statssummary2d) float8
sum_x(summary statssummary2d) float8
sum_y(summary statssummary2d) float8
stddev_x(summary statssummary2d) float8
stddev_y(summary statssummary2d) float8
variance_x(summary statssummary2d) float8
variance_y(summary statssummary2d) float8
skewness_x(summary statssummary2d) float8
skewness_y(summary statssummary2d) float8
kurtosis_x(summary statssummary2d) float8
kurtosis_y(summary statssummary2d) float8

stddev(summary statssummary1d, method text) float8
stddev(summary statssummary2d, method text) float8
variance(summary statssummary1d, method text) float8
variance(summary statssummary2d, method text) float8
skewness(summary statssummary1d, method text) float8
skewness(summary statssummary2d, method text) float8
kurtosis(summary statssummary1d, method text) float8
kurtosis(summary statssummary2d, method text) float8

covariance(summary statssummary2d, method text) float8

delta(summary counter_summary) float8
delta(summary gauge_summary) float8
rate(summary counter_summary) float8
idelta_left(summary counter_summary) float8
idelta_right(summary counter_summary) float8
irate_left(summary counter_summary) float8
irate_right(summary counter_summary) float8
time_delta(summary counter_summary) float8

num_changes(summary counter_summary) int8
num_resets(summary counter_summary) int8
num_elements(summary counter_summary) int8

first_val(summary counter_summary) float8
last_val(summary counter_summary) float8
first_time(summary counter_summary) timestamptz
last_time(summary counter_summary) timestamptz

slope(summary counter_summary) float8
intercept(summary counter_summary) float8
corr(summary counter_summary) float8
counter_zero_time(summary counter_summary) timestamptz

extrapolated_delta(summary counter_summary, method text) float8
extrapolated_rate(summary counter_summary, method text) float8

interpolated_delta(summary counter_summary, start timestamptz, duration interval, prev counter_summary, next counter_summary) float8 called-on-null
interpolated_rate(summary counter_summary, start timestamptz, duration interval, prev counter_summary, next counter_summary) float8 called-on-null

with_bounds(summary counter_summary, bounds tstzrange) counter_summary
counter_agg(ts timestamptz, value float8, bounds tstzrange) counter_summary called-on-null aggregate

candlestick_agg(ts timestamptz, price float8, volume float8) candlestick called-on-null aggregate
candlestick(ts timestamptz, open float8, high float8, low float8, close float8, volume float8) candlestick
state_agg(ts timestamptz, state text) state_summary called-on-null aggregate
state_agg(ts timestamptz, state int8) state_summary called-on-null aggregate
compact_state_agg(ts timestamptz, state text) compact_state_agg called-on-null aggregate
compact_state_agg(ts timestamptz, state int8) compact_state_agg called-on-null aggregate
hyperloglog(precision int4, value any) hyperloglog called-on-null aggregate
approx_count_distinct(hll hyperloglog) int8
distinct_count(hll hyperloglog) int8
stderror(hll hyperloglog) float8
tdigest(buckets int4, value float8) tdigest called-on-null aggregate
approx_percentile(percentile float8, digest tdigest) float8
uddsketch(buckets int4, alpha float8, value float8) uddsketch called-on-null aggregate
approx_percentile(percentile float8, sketch uddsketch) float8
error(sketch uddsketch) float8
percentile_agg(value float8) uddsketch called-on-null aggregate

open(summary candlestick) float8
high(summary candlestick) float8
low(summary candlestick) float8
close(summary candlestick) float8
open_time(summary candlestick) timestamptz
close_time(summary candlestick) timestamptz
high_time(summary candlestick) timestamptz
low_time(summary candlestick) timestamptz
volume(summary candlestick) float8
vwap(summary candlestick) float8

approx_percentile_rank(value float8, sketch tdigest) float8
approx_percentile_rank(value float8, sketch uddsketch) float8
mean(summary tdigest) float8
mean(summary uddsketch) float8
num_vals(summary tdigest) float8
num_vals(summary uddsketch) float8
min_val(summary tdigest) float8
max_val(summary tdigest) float8

time_weight(method text, ts timestamptz, value float8) time_weight_summary called-on-null aggregate
average(summary time_weight_summary) float8
integral(summary time_weight_summary) float8
integral(summary time_weight_summary, unit text) float8
first_val(summary time_weight_summary) float8
last_val(summary time_weight_summary) float8
first_time(summary time_weight_summary) timestamptz
last_time(summary time_weight_summary) timestamptz

interpolated_average(summary time_weight_summary, start timestamptz, interval interval, prev time_weight_summary, next time_weight_summary) float8
interpolated_integral(summary time_weight_summary, start timestamptz, interval interval, prev time_weight_summary, next time_weight_summary, unit text) float8

rollup(summary time_weight_summary) time_weight_summary called-on-null aggregate

live_ranges(summary heartbeat) heartbeat_range_record never-null returns-set
dead_ranges(summary heartbeat) heartbeat_range_record never-null returns-set
uptime(summary heartbeat) interval
downtime(summary heartbeat) interval
live_at(summary heartbeat, ts timestamptz) bool
num_live_ranges(summary heartbeat) int8
num_gaps(summary heartbeat) int8
interpolated_uptime(summary heartbeat, prev heartbeat) interval
interpolated_downtime(summary heartbeat, prev heartbeat) interval
trim_to(summary heartbeat, start timestamptz, duration interval) heartbeat
interpolate(summary heartbeat, prev heartbeat) heartbeat called-on-null

into_values(summary state_summary) state_duration_record never-null returns-set
duration_in(summary state_summary, state text) interval
duration_in(summary state_summary, state text, start timestamptz, duration interval) interval

state_timeline(summary state_summary) state_timeline_record never-null returns-set
state_int_timeline(summary state_summary) state_int_timeline_record never-null returns-set

state_periods(summary state_summary, state text) state_period_record never-null returns-set
state_int_periods(summary state_summary, state int8) state_int_period_record never-null returns-set

state_at(summary state_summary, ts timestamptz) text
state_at_int(summary state_summary, ts timestamptz) int8

interpolated_state_timeline(summary state_summary, start timestamptz, interval interval, prev state_summary, next state_summary) state_timeline_record never-null returns-set
interpolated_state_periods(summary state_summary, state text, start timestamptz, interval interval, prev state_summary, next state_summary) state_period_record never-null returns-set
interpolated_duration_in(summary state_summary, state text, start timestamptz, interval interval, prev state_summary, next state_summary) interval

into_values(summary compact_state_agg) state_duration_record never-null returns-set
duration_in(summary compact_state_agg, state text) interval
duration_in(summary compact_state_agg, state text, start timestamptz, duration interval) interval

state_timeline(summary compact_state_agg) state_timeline_record never-null returns-set
state_int_timeline(summary compact_state_agg) state_int_timeline_record never-null returns-set

state_periods(summary compact_state_agg, state text) state_period_record never-null returns-set
state_int_periods(summary compact_state_agg, state int8) state_int_period_record never-null returns-set

state_at(summary compact_state_agg, ts timestamptz) text
state_at_int(summary compact_state_agg, ts timestamptz) int8

interpolated_state_timeline(summary compact_state_agg, start timestamptz, interval interval, prev compact_state_agg, next compact_state_agg) state_timeline_record never-null returns-set
interpolated_state_periods(summary compact_state_agg, state text, start timestamptz, interval interval, prev compact_state_agg, next compact_state_agg) state_period_record never-null returns-set
interpolated_duration_in(summary compact_state_agg, state text, start timestamptz, interval interval, prev compact_state_agg, next compact_state_agg) interval

add_compression_policy(hypertable regclass, compress_after any, if_not_exists bool, schedule_interval interval, initial_start timestamptz, timezone text, compress_created_before interval) int4 never-null min-arguments=2 modifies-data
remove_compression_policy(hypertable regclass) bool never-null modifies-data

add_retention_policy(relation regclass, drop_after any, schedule_interval interval, initial_start timestamptz, timezone text, if_not_exists bool, drop_created_before interval) int4 never-null min-arguments=2 modifies-data
remove_retention_policy(hypertable regclass) bool never-null modifies-data

add_continuous_aggregate_policy(continuous_aggregate regclass, start_offset any, end_offset any, schedule_interval interval, if_not_exists bool, initial_start timestamptz, timezone text, include_tiered_data bool, buckets_per_batch int4, max_batches_per_execution int4, refresh_newest_first bool) int4 never-null min-arguments=4 modifies-data
remove_continuous_aggregate_policy(continuous_aggregate regclass) bool never-null modifies-data

add_reorder_policy(hypertable regclass, index_name text, if_not_exists bool, initial_start timestamptz, timezone text) int4 never-null min-arguments=2 modifies-data
remove_reorder_policy(hypertable regclass, if_exists bool) void never-null min-arguments=1 modifies-data

add_job(proc regproc, schedule_interval interval, config jsonb, initial_start timestamptz, scheduled bool, check_config regproc, fixed_schedule bool, timezone text) int4 never-null min-arguments=2 modifies-data
alter_job(job_id int4, schedule_interval interval, max_runtime interval, max_retries int4, retry_period interval, scheduled bool, config jsonb, next_start timestamptz, if_exists bool, check_config regproc, fixed_schedule bool, initial_start timestamptz, timezone text) alter_job_record never-null min-arguments=1 modifies-data
run_job(job_id int4) void never-null modifies-data
delete_job(job_id int4) void never-null modifies-data

create_hypertable(table regclass, time_column text) create_hypertable_record never-null modifies-data
create_hypertable(table regclass, time_column text, partition_column text, number_partitions int4) create_hypertable_record never-null min-arguments=2 modifies-data
add_dimension(hypertable regclass, column text, number_partitions int4) add_dimension_record never-null modifies-data
add_dimension(hypertable regclass, column text, chunk_time_interval interval) add_dimension_record never-null modifies-data
set_chunk_time_interval(hypertable regclass, chunk_time_interval interval) void never-null modifies-data
reorder_chunk(chunk regclass, index regclass) void never-null min-arguments=1 modifies-data

drop_chunks(hypertable regclass, older_than timestamptz) text never-null returns-set modifies-data
drop_chunks(relation regclass, older_than any, newer_than any, verbose bool, created_before any, created_after any) text never-null min-arguments=1 returns-set modifies-data
show_chunks(hypertable regclass) regclass never-null returns-set
show_chunks(relation regclass, older_than any, newer_than any, created_before any, created_after any) regclass never-null min-arguments=1 returns-set

compress_chunk(chunk regclass) regclass never-null modifies-data
decompress_chunk(chunk regclass) regclass never-null modifies-data
refresh_continuous_aggregate(continuous_aggregate regclass!, start timestamptz, finish timestamptz) void never-null modifies-data

attach_tablespace(tablespace text, hypertable regclass, if_not_attached bool) void never-null min-arguments=2 modifies-data
detach_tablespace(tablespace text, hypertable regclass, if_attached bool) int4 never-null min-arguments=2 modifies-data
detach_tablespaces(hypertable regclass) int4 never-null modifies-data
move_chunk(chunk regclass, destination_tablespace name, index_destination_tablespace name, reorder_index regclass, verbose bool) void never-null min-arguments=2 modifies-data

merge_chunks(chunk1 regclass, chunk2 regclass, concurrently bool) void never-null modifies-data
merge_chunks(chunks regclass[], concurrently bool) void never-null modifies-data
merge_chunks_concurrently(chunks regclass[]) void never-null variadic=0 modifies-data
split_chunk(chunk regclass, split_at timestamptz) void never-null modifies-data
attach_chunk(hypertable regclass, chunk regclass, slices jsonb) void never-null modifies-data
detach_chunk(chunk regclass) void never-null modifies-data
create_chunk(hypertable regclass, slices jsonb, schema_name text, table_name text, chunk_table regclass) create_chunk_record never-null modifies-data
drop_chunk(chunk regclass) bool never-null modifies-data

hypertable_approximate_size(hypertable regclass) int8
hypertable_approximate_detailed_size(hypertable regclass) hypertable_approximate_detailed_size_record never-null returns-set

convert_to_columnstore(chunk regclass, if_not_columnstore bool, recompress bool) void never-null modifies-data
convert_to_rowstore(chunk regclass, if_compressed bool) void never-null modifies-data
recompress_chunk(chunk regclass, if_not_compressed bool) void never-null modifies-data
rebuild_columnstore(chunk regclass) void never-null modifies-data
hypertable_columnstore_stats(hypertable regclass) hypertable_compression_stats_record never-null returns-set
chunk_columnstore_stats(hypertable regclass) chunk_compression_stats_record never-null returns-set

approximate_row_count(hypertable regclass) int8 never-null
hypertable_size(hypertable regclass) int8
chunk_size(chunk regclass) int8
hypertable_index_size(index regclass) int8
hypertable_detailed_size(hypertable regclass) hypertable_detailed_size_record never-null returns-set
chunks_detailed_size(hypertable regclass) chunk_detailed_size_record never-null returns-set
hypertable_compression_stats(hypertable regclass) hypertable_compression_stats_record never-null returns-set
chunk_compression_stats(hypertable regclass) chunk_compression_stats_record never-null returns-set

rollup(summary statssummary1d) statssummary1d called-on-null aggregate
rollup(summary statssummary2d) statssummary2d called-on-null aggregate
rollup(summary counter_summary) counter_summary called-on-null aggregate
rollup(summary gauge_summary) gauge_summary called-on-null aggregate
rollup(summary candlestick) candlestick called-on-null aggregate
rollup(summary state_summary) state_summary called-on-null aggregate
rollup(summary compact_state_agg) compact_state_agg called-on-null aggregate
rollup(summary heartbeat) heartbeat called-on-null aggregate
rollup(summary hyperloglog) hyperloglog called-on-null aggregate
rollup(summary tdigest) tdigest called-on-null aggregate
rollup(summary uddsketch) uddsketch called-on-null aggregate

freq_agg(frequency float8, value int8) space_saving_bigint_aggregate called-on-null aggregate
freq_agg(frequency float8, value text) space_saving_text_aggregate called-on-null aggregate
mcv_agg(count int4, value int8) space_saving_bigint_aggregate called-on-null aggregate
mcv_agg(count int4, value text) space_saving_text_aggregate called-on-null aggregate
mcv_agg(count int4, skew float8, value int8) space_saving_bigint_aggregate called-on-null aggregate
mcv_agg(count int4, skew float8, value text) space_saving_text_aggregate called-on-null aggregate
raw_freq_agg(frequency float8, value any) space_saving_aggregate called-on-null aggregate
raw_mcv_agg(count int4, value any) space_saving_aggregate called-on-null aggregate
raw_mcv_agg(count int4, skew float8, value any) space_saving_aggregate called-on-null aggregate

topn(agg space_saving_bigint_aggregate) int8 never-null returns-set
topn(agg space_saving_bigint_aggregate, n int4) int8 never-null returns-set
topn(agg space_saving_text_aggregate) text never-null returns-set
topn(agg space_saving_text_aggregate, n int4) text never-null returns-set
topn(agg space_saving_aggregate, ty any) any never-null returns-set
topn(agg space_saving_aggregate, n int4, ty any) any never-null returns-set

into_values(agg space_saving_bigint_aggregate) space_saving_bigint_aggregate_into_values_record never-null returns-set
into_values(agg space_saving_bigint_aggregate, witness any) space_saving_bigint_aggregate_into_values_record never-null returns-set
into_values(agg space_saving_text_aggregate) space_saving_text_aggregate_into_values_record never-null returns-set
into_values(agg space_saving_text_aggregate, witness any) space_saving_text_aggregate_into_values_record never-null returns-set
into_values(agg space_saving_aggregate) space_saving_aggregate_into_values_record never-null returns-set
into_values(agg space_saving_aggregate, witness any) space_saving_aggregate_into_values_record never-null returns-set

max_frequency(agg space_saving_bigint_aggregate, value any) float8
min_frequency(agg space_saving_bigint_aggregate, value any) float8
max_frequency(agg space_saving_text_aggregate, value any) float8
min_frequency(agg space_saving_text_aggregate, value any) float8
max_frequency(agg space_saving_aggregate, value any) float8
min_frequency(agg space_saving_aggregate, value any) float8

rollup(summary space_saving_bigint_aggregate) space_saving_bigint_aggregate called-on-null aggregate
rollup(summary space_saving_text_aggregate) space_saving_text_aggregate called-on-null aggregate
rollup(summary space_saving_aggregate) space_saving_aggregate called-on-null aggregate

min_n(value float8, n int4) min_n_state called-on-null aggregate
max_n(value float8, n int4) min_n_state called-on-null aggregate
min_n_by(value float8, by any, n int4) min_n_by_state called-on-null aggregate
max_n_by(value float8, by any, n int4) min_n_by_state called-on-null aggregate
min_n(value int8, n int4) min_n_state called-on-null aggregate
max_n(value int8, n int4) min_n_state called-on-null aggregate
min_n_by(value int8, by any, n int4) min_n_by_state called-on-null aggregate
max_n_by(value int8, by any, n int4) min_n_by_state called-on-null aggregate
min_n(value text, n int4) min_n_state called-on-null aggregate
max_n(value text, n int4) min_n_state called-on-null aggregate
min_n_by(value text, by any, n int4) min_n_by_state called-on-null aggregate
max_n_by(value text, by any, n int4) min_n_by_state called-on-null aggregate
min_n(value timestamptz, n int4) min_n_state called-on-null aggregate
max_n(value timestamptz, n int4) min_n_state called-on-null aggregate
min_n_by(value timestamptz, by any, n int4) min_n_by_state called-on-null aggregate
max_n_by(value timestamptz, by any, n int4) min_n_by_state called-on-null aggregate
min_n(value any, n int4) min_n_state called-on-null aggregate
max_n(value any, n int4) min_n_state called-on-null aggregate
min_n_by(value any, by any, n int4) min_n_by_state called-on-null aggregate
max_n_by(value any, by any, n int4) min_n_by_state called-on-null aggregate

into_array(state min_n_state) any[]
into_array(state min_n_by_state) any[]
into_values(state min_n_state) any never-null returns-set
into_values(state min_n_by_state) min_n_by_record never-null returns-set

rollup(summary min_n_state) min_n_state called-on-null aggregate
rollup(summary min_n_by_state) min_n_by_state called-on-null aggregate

lttb(ts timestamptz, value float8, resolution int4) timevector_tstz_f64 called-on-null aggregate
lttb(series timevector_tstz_f64, threshold int4) timevector_tstz_f64
asap_smooth(ts timestamptz, value float8, resolution int4) timevector_tstz_f64 called-on-null aggregate
asap_smooth(series timevector_tstz_f64, resolution int4) timevector_tstz_f64
timevector(ts timestamptz, value float8) timevector_tstz_f64 called-on-null aggregate
rollup(summary timevector_tstz_f64) timevector_tstz_f64 called-on-null aggregate
unnest(series timevector_tstz_f64) timevector_point_record never-null returns-set

count_min_sketch(value text, error float8, probability float8) count_min_sketch called-on-null aggregate
approx_count(item text, agg count_min_sketch) int8
rollup(summary count_min_sketch) count_min_sketch called-on-null aggregate

saturating_add(a int4, b int4) int4
saturating_sub(a int4, b int4) int4
saturating_mul(a int4, b int4) int4
saturating_add_pos(a int4, b int4) int4
saturating_sub_pos(a int4, b int4) int4
saturating_mul_pos(a int4, b int4) int4

timescaledb_pre_restore() void never-null
timescaledb_post_restore() void never-null

approx_percentile(percentile float8) accessorapproxpercentile
approx_percentile_rank(value float8) accessorapproxpercentilerank
approx_percentiles(unit float8[]) accessorpercentilearray
average() accessoraverage
average_x() accessoraveragex
average_y() accessoraveragey
close() accessorclose
close_time() accessorclosetime
corr() accessorcorr
counter_zero_time() accessorcounterzerotime
covariance(method text?) accessorcovar
dead_ranges() accessordeadranges
delta() accessordelta
determination_coeff() accessordeterminationcoeff
distinct_count() accessordistinctcount
downtime() accessordowntime
duration_in(state int8) accessordurationinint
duration_in(state int8, start timestamptz, interval interval?) accessordurationinrangeint called-on-null
duration_in(state text) accessordurationin
duration_in(state text, start timestamptz, interval interval?) accessordurationinrange called-on-null
error() accessorerror
extrapolated_delta(method text) accessorextrapolateddelta
extrapolated_rate(method text) accessorextrapolatedrate
first_time() accessorfirsttime
first_val() accessorfirstval
high() accessorhigh
high_time() accessorhightime
idelta_left() accessorideltaleft
idelta_right() accessorideltaright
integral(unit text?) accessorintegral
intercept() accessorintercept
interpolate(prev heartbeat) heartbeatinterpolateaccessor called-on-null
interpolated_average(start timestamptz, duration interval, prev time_weight_summary?, next time_weight_summary?) timeweightinterpolatedaverageaccessor called-on-null
interpolated_delta(start timestamptz, duration interval, prev counter_summary, next counter_summary) counterinterpolateddeltaaccessor called-on-null
interpolated_downtime(prev heartbeat) heartbeatinterpolateddowntimeaccessor called-on-null
interpolated_duration_in(state int8, start timestamptz, interval interval, prev state_summary) accessorinterpolateddurationinint called-on-null
interpolated_duration_in(state text, start timestamptz, interval interval, prev state_summary) accessorinterpolateddurationin called-on-null
interpolated_integral(start timestamptz, interval interval, prev time_weight_summary?, next time_weight_summary?, unit text?) timeweightinterpolatedintegralaccessor called-on-null
interpolated_rate(start timestamptz, duration interval, prev counter_summary, next counter_summary) counterinterpolatedrateaccessor called-on-null
interpolated_state_int_timeline(start timestamptz, interval interval, prev state_summary) accessorinterpolatedstateinttimeline called-on-null
interpolated_state_periods(state int8, start timestamptz, interval interval, prev state_summary) accessorinterpolatedstateperiodsint called-on-null
interpolated_state_periods(state text, start timestamptz, interval interval, prev state_summary) accessorinterpolatedstateperiods called-on-null
interpolated_state_timeline(start timestamptz, interval interval, prev state_summary) accessorinterpolatedstatetimeline called-on-null
interpolated_uptime(prev heartbeat) heartbeatinterpolateduptimeaccessor called-on-null
into_array() accessorintoarray
into_int_values() accessorintointvalues
into_values() accessorintovalues
irate_left() accessorirateleft
irate_right() accessorirateright
kurtosis(method text?) accessorkurtosis
kurtosis_x(method text?) accessorkurtosisx
kurtosis_y(method text?) accessorkurtosisy
last_time() accessorlasttime
last_val() accessorlastval
live_at(ts timestamptz) accessorliveat
live_ranges() accessorliveranges
low() accessorlow
low_time() accessorlowtime
max_frequency(value int8) accessormaxfrequencyint
max_val() accessormaxval
mean() accessormean
min_frequency(value int8) accessorminfrequencyint
min_val() accessorminval
num_changes() accessornumchanges
num_elements() accessornumelements
num_gaps() accessornumgaps
num_live_ranges() accessornumliveranges
num_resets() accessornumresets
num_vals() accessornumvals
open() accessoropen
open_time() accessoropentime
rate() accessorrate
skewness(method text?) accessorskewness
skewness_x(method text?) accessorskewnessx
skewness_y(method text?) accessorskewnessy
slope() accessorslope
state_at(time timestamptz) accessorstateat
state_at_int(time timestamptz) accessorstateatint
state_int_timeline() accessorstateinttimeline
state_periods(state int8) accessorstateperiodsint
state_periods(state text) accessorstateperiods
state_timeline() accessorstatetimeline
stddev(method text?) accessorstddev
stddev_x(method text?) accessorstddevx
stddev_y(method text?) accessorstddevy
stderror() accessorstderror
sum() accessorsum
sum_x() accessorsumx
sum_y() accessorsumy
time_delta() accessortimedelta
topn() accessortopn
topn(count int8) accessortopncount
trim_to(start timestamptz, duration interval?) heartbeattrimtoaccessor called-on-null
unnest() accessorunnest
uptime() accessoruptime
variance(method text?) accessorvariance
variance_x(method text?) accessorvariancex
variance_y(method text?) accessorvariancey
with_bounds(bounds tstzrange) accessorwithbounds
x_intercept() accessorxintercept
