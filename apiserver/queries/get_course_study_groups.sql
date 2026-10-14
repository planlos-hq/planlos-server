select
    sg.id,
    sg.semester_season::text,
    sg.semester_year,
    sg.student_count,
    sg.degree_program
from
    course c
    left join course_study_group csg on c.id = csg.course
    left join study_group sg on csg.study_group = sg.id
where
    c.id = $1
order by
    sg.semester_year desc,
    sg.semester_season
