#!/bin/bash

BIN_DIR=/usr/bin
DATA_DIR=/var/lib/influxdb-relay
LOG_DIR=/var/log/influxdb-relay
SCRIPT_DIR=/usr/lib/influxdb-relay/scripts
LOGROTATE_DIR=/etc/logrotate.d

function install_init {
    cp -f $SCRIPT_DIR/init.sh /etc/init.d/influxdb-relay
    chmod +x /etc/init.d/influxdb-relay
}

function install_systemd {
    cp -f $SCRIPT_DIR/influxdb-relay.service /lib/systemd/system/influxdb-relay.service
    systemctl enable influxdb-relay
}

function install_update_rcd {
    update-rc.d influxdb-relay defaults
}

function install_chkconfig {
    chkconfig --add influxdb-relay
}

id influxdb-relay &>/dev/null
if [[ $? -ne 0 ]]; then
    useradd --system -U -M influxdb-relay -s /bin/false -d $DATA_DIR
fi

chown -R -L influxdb-relay:influxdb-relay $DATA_DIR
chown -R -L influxdb-relay:influxdb-relay $LOG_DIR

# Remove legacy symlink, if it exists
if [[ -L /etc/init.d/influxdb-relay ]]; then
    rm -f /etc/init.d/influxdb-relay
fi

# Distribution-specific logic
if [[ -f /etc/redhat-release ]]; then
    # RHEL-variant logic
    which systemctl &>/dev/null
    if [[ $? -eq 0 ]]; then
	install_systemd
    else
	# Assuming sysv
	install_init
	install_chkconfig
    fi
elif [[ -f /etc/debian_version ]]; then
    # Debian/Ubuntu logic
    which systemctl &>/dev/null
    if [[ $? -eq 0 ]]; then
	install_systemd
    else
	# Assuming sysv
	install_init
	install_update_rcd
    fi
elif [[ -f /etc/os-release ]]; then
    source /etc/os-release
    if [[ $ID = "amzn" ]]; then
	# Amazon Linux logic
	install_init
	install_chkconfig
    fi
fi
