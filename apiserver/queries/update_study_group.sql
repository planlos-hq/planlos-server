update
    study_group
set
    semester_season = coalesce($1::semester_season, semester_season),
    semester_year = coalesce($2, semester_year),
    student_count = coalesce($3, student_count),
    degree_program = coalesce($4, degree_program),
    extra_data = coalesce($5, extra_data)
where
    id = $6
returning
    id,
    semester_season::text,
    semester_year,
    student_count,
    degree_program,
    extra_data
