# NetXMS Data Source Plugin for Grafana

[![Grafana Marketplace](https://img.shields.io/badge/dynamic/json?logo=grafana&query=$.version&url=https://grafana.com/api/plugins/radensolutions-netxms-datasource&label=Marketplace&prefix=v&color=F47A20)](https://grafana.com/grafana/plugins/radensolutions-netxms-datasource/)
[![Downloads](https://img.shields.io/badge/dynamic/json?logo=grafana&query=$.downloads&url=https://grafana.com/api/plugins/radensolutions-netxms-datasource&label=Downloads&color=F47A20)](https://grafana.com/grafana/plugins/radensolutions-netxms-datasource/)
[![Grafana](https://img.shields.io/badge/dynamic/json?logo=grafana&query=$.grafanaDependency&url=https://grafana.com/api/plugins/radensolutions-netxms-datasource&label=Grafana&color=F47A20)](https://grafana.com/grafana/plugins/radensolutions-netxms-datasource/)
[![License](https://img.shields.io/github/license/netxms/grafana-datasource)](https://github.com/netxms/grafana-datasource/blob/master/LICENSE)

The **NetXMS Data Source Plugin** enables Grafana to visualize data from NetXMS, an open-source network management and monitoring platform. This plugin allows you to query alarms, DCI values, summary tables, and more, directly from your NetXMS server.

## Features

- Query NetXMS alarms
- Show DCI values
- Query summary tables
- Object status map
- Support for custom queries and dynamic dashboards
- Secure API key authentication

## Requirements

- Grafana 10.4.0 or later
- NetXMS server with WebAPI enabled

## Getting Started

### NetXMS Server Configuration

1. Enable WebAPI on your NetXMS server by adding `Module=webapi` to `netxmsd.conf`
2. Restart the NetXMS server
3. Note: WebAPI uses unencrypted HTTP by default. Consider using nginx, reproxy, traefik, or another SSL offloading solution
4. In the User Management view, select the user for Grafana and issue a new API key

### Grafana Configuration

1. Navigate to **Configuration > Data Sources**
2. Add "NetXMS" as a new data source
3. Configure the following:
   - **Server Address:** URL of your NetXMS server (e.g., `http://localhost:8000`)
   - **API Key:** Your NetXMS API key issued in the previous step

## Usage

1. Create a new dashboard and add a panel
2. Select the NetXMS data source
3. Choose the query type (alarms, DCI values, summary tables, etc.)
4. Configure query parameters as needed
5. Visualize your network data in real time

## Screenshots

![Alarms Dashboard](https://raw.githubusercontent.com/netxms/grafana-datasource/master/src/img/dashboard-alarms.png)

![DCI Graph Dashboard](https://raw.githubusercontent.com/netxms/grafana-datasource/master/src/img/dashboard-graph.png)

![Object Query Dashboard](https://raw.githubusercontent.com/netxms/grafana-datasource/master/src/img/dashboard-object-query.png)

![Status Map Dashboard](https://raw.githubusercontent.com/netxms/grafana-datasource/master/src/img/dashboard-statu-map.png)

![Summary Table Dashboard](https://raw.githubusercontent.com/netxms/grafana-datasource/master/src/img/dashboard-summary-table.png)

## Documentation

- [NetXMS Documentation](https://netxms.com/documentation)
- [NetXMS Website](https://netxms.com/)

## Support

- [Forum](https://netxms.org/forum)
- [Telegram](https://telegram.me/netxms)
- [Issue Tracker](https://dev.raden.solutions/projects/netxms/)
