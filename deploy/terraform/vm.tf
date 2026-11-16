# The Lightsail VM that runs Postgres and Centrifugo belongs to INTODAN/mpl
# (#232): its Terraform imported the instances, the static IP and its
# attachment when it took the VM over (#231), and creates the firewall once
# this configuration has let go of it. Island Battles is a tenant there, with
# its database and Centrifugo's API key (deploy/README.md, "The backend's
# settings").
#
# These blocks take the VM's resources out of this configuration's state
# without destroying them: the plan lists them as removed from the state, and
# changes nothing in AWS. check_deploy_plan.py refuses any plan that would
# destroy a Lightsail resource or the account's GitHub OIDC provider, which
# INTODAN/mpl's roles trust too.

removed {
  from = aws_lightsail_instance.vm

  lifecycle {
    destroy = false
  }
}

removed {
  from = aws_lightsail_instance_public_ports.vm

  lifecycle {
    destroy = false
  }
}

removed {
  from = aws_lightsail_static_ip.vm

  lifecycle {
    destroy = false
  }
}

removed {
  from = aws_lightsail_static_ip_attachment.vm

  lifecycle {
    destroy = false
  }
}

removed {
  from = aws_lightsail_key_pair.vm

  lifecycle {
    destroy = false
  }
}
