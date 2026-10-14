select
    c.id,
    c.number,
    c.name,
    c.sws,
    c.credit_points,
    c.abbreviation,
    c.extra_data
from
    study_group_template t
    left join study_group_template_course tc on t.id = tc.template
    left join course c on tc.course = c.id
where
    t.id = $1
order by
    c.number
