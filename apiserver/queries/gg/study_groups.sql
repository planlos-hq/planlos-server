select
    id,
    semester_season::text,
    semester_year,
    student_count,
    degree_program,
    extra_data
from
    study_group
order by
    semester_year desc,
    semester_season
