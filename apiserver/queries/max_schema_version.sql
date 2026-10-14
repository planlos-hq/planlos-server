select
    coalesce(max(version), 0)
from
    schema_version_history
