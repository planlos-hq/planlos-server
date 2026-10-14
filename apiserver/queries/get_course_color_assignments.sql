select
    cc.id,
    cc.name,
    cc.hex
from
    course c
    left join course_color_assignment cca on c.id = cca.course
    left join course_color cc on cc.id = cca.color
where
    c.id = $1
order by
    cc.name
