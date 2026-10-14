insert into study_group (semester_season, semester_year, student_count,
    degree_program, extra_data)
    values ($1::semester_season, $2, $3, $4, $5)
returning
    id, semester_season::text, semester_year, student_count, degree_program, extra_data
