class Volume:
    def __init__(self, vol_size_bytes, vol_id, vol_name, array_address, pool_name, copy_source_id, array_type, space_efficiency):
        self.capacity_bytes = vol_size_bytes
        self.id = vol_id
        self.name = vol_name
        self.array_address = array_address
        self.pool_name = pool_name
        self.copy_source_id = copy_source_id
        self.array_type = array_type
        self.space_efficiency = space_efficiency

class Snapshot:
    def __init__(self, capacity_bytes, snapshot_id, snapshot_name, array_address, volume_name, is_ready, array_type):
        self.capacity_bytes = capacity_bytes
        self.id = snapshot_id
        self.name = snapshot_name
        self.array_address = array_address
        self.volume_name = volume_name
        self.is_ready = is_ready
        self.array_type = array_type


class Host:
    def __init__(self, host_id, host_name, iscsi_names, wwns):
        self.id = host_id
        self.name = host_name
        self.iscsi_names = iscsi_names
        self.wwns = wwns
