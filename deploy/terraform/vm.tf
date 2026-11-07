# The Lightsail VM that runs Postgres and Centrifugo (#26). Ansible configures
# it: scripts/vm.sh deploy "$(scripts/aws.sh output static_ip)".

resource "aws_lightsail_key_pair" "vm" {
  count = var.ssh_public_key == null ? 0 : 1

  name       = var.name
  public_key = var.ssh_public_key
}

resource "aws_lightsail_instance" "vm" {
  for_each = var.vm_instances

  name              = each.key
  availability_zone = coalesce(each.value.availability_zone, "${var.region}a")
  blueprint_id      = each.value.blueprint_id
  bundle_id         = each.value.bundle_id
  # Null uses the region's default key pair.
  key_pair_name = one(aws_lightsail_key_pair.vm[*].name)
  # vm.<domain> is only an A record, so the instance needs no IPv6 address and
  # its firewall only has IPv4 rules.
  ip_address_type = "ipv4"

  # No automatic snapshots: the database is backed up to S3 instead
  # (backups.tf). Snapshots stored the whole disk seven times over, while
  # everything but Postgres's data is rebuilt by the deploys. Lightsail keeps
  # listing a disabled add-on, so it stays declared as Disabled: without the
  # block, every plan would want to change the instance, which stops every
  # backend deploy (check_deploy_plan.py). snapshot_time is ignored.
  add_on {
    type          = "AutoSnapshot"
    snapshot_time = "01:00"
    status        = "Disabled"
  }

  lifecycle {
    # Changing the key pair would replace the instance and delete its disk.
    # A new key only applies to new instances.
    ignore_changes = [key_pair_name]
  }
}

# Lightsail's firewall is the only one: ports that Docker publishes bypass a
# host firewall. These rules replace every other rule on the instance.
resource "aws_lightsail_instance_public_ports" "vm" {
  for_each = aws_lightsail_instance.vm

  instance_name = each.value.name

  port_info {
    protocol  = "tcp"
    from_port = 22
    to_port   = 22
    cidrs     = var.ssh_allowed_cidrs
  }

  # Centrifugo: browsers' WebSocket connections, the backend's HTTP API calls
  # and Let's Encrypt's TLS-ALPN challenge. Port 80 stays closed.
  port_info {
    protocol  = "tcp"
    from_port = 443
    to_port   = 443
    cidrs     = ["0.0.0.0/0"]
  }

  # Postgres, TLS only. Lambda's outbound addresses aren't fixed, so it's open
  # to everyone, and pg_hba.conf admits only two roles over TLS, each with its
  # password: the backend's, and the read-only role of the admin MCP server
  # that the operator runs on their own machine (#79).
  port_info {
    protocol  = "tcp"
    from_port = var.postgres_port
    to_port   = var.postgres_port
    cidrs     = ["0.0.0.0/0"]
  }
}

# The static IP outlives the instances: vm.<domain> points at it, and moving
# to a new VM moves it to the new instance.
resource "aws_lightsail_static_ip" "vm" {
  name = "${var.name}-vm"

  lifecycle {
    # Releasing it gives the address away for good, and vm.<domain> with it.
    prevent_destroy = true
  }
}

resource "aws_lightsail_static_ip_attachment" "vm" {
  static_ip_name = aws_lightsail_static_ip.vm.name
  instance_name  = aws_lightsail_instance.vm[var.static_ip_instance].name
}
