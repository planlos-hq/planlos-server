select
    id,
    semester_season::text,
    semester_year,
    student_count,
    degree_program,
    extra_data
from
    study_group
where
    id = $1
