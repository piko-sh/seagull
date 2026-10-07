-- sgl.query(Compute, many)
SELECT times_two(21) AS small, times_two(value) AS doubled, add_xy(value, {offset:Int32}) AS shifted FROM readings;
