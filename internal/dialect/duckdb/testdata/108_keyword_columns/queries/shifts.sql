-- sgl.query(name: ShiftColumns, command: many)
SELECT current_role, localtime, user, current_date FROM shifts;

-- sgl.query(name: KeywordValues, command: one)
SELECT current_date AS today, user AS who, current_user AS account, current_timestamp AS stamp;

-- sgl.query(name: ColumnsOfOwnQueryOnly, command: many)
SELECT id, (SELECT current_date) AS today FROM shifts;

-- sgl.query(name: QualifiedColumns, command: many)
SELECT shifts.current_date, other.user FROM shifts JOIN shifts AS other ON other.id = shifts.id;

-- sgl.query(name: ViewColumns, command: many)
SELECT shift_days.current_date, shift_days.role_number, crew_days.today FROM shift_days, crew_days;

-- sgl.query(name: ColumnInCondition, command: many)
SELECT id FROM shifts WHERE current_date = $1;

-- sgl.query(name: ValuesBesideOtherTable, command: many)
SELECT current_date AS today, user AS who FROM crew;

-- sgl.query(name: UserAsAlias, command: many)
SELECT user.id FROM crew AS user;
