// 励联 LiLink 技术白皮书图表：会话路径分布
(function() {
  var style = getComputedStyle(document.documentElement);
  var accent = style.getPropertyValue('--accent').trim();
  var accent2 = style.getPropertyValue('--accent2').trim();
  var ink = style.getPropertyValue('--ink').trim();
  var muted = style.getPropertyValue('--muted').trim();

  var chart = echarts.init(document.getElementById('chart-path'), null, { renderer: 'svg' });
  chart.setOption({
    animation: false,
    tooltip: { trigger: 'item', appendToBody: true, formatter: '{b}：{c}%（{d}%）' },
    legend: {
      bottom: 0,
      textStyle: { color: muted, fontFamily: 'inherit' },
      itemWidth: 14, itemHeight: 10, icon: 'roundRect'
    },
    series: [{
      name: '会话路径',
      type: 'pie',
      radius: ['52%', '74%'],
      center: ['50%', '44%'],
      avoidLabelOverlap: true,
      itemStyle: { borderColor: '#fff', borderWidth: 2, borderRadius: 6 },
      label: {
        show: true, formatter: '{b}\n{c}%',
        color: ink, fontSize: 14, fontWeight: 600, lineHeight: 20
      },
      labelLine: { lineStyle: { color: muted } },
      data: [
        { value: 90, name: 'P2P 直连', itemStyle: { color: accent } },
        { value: 10, name: '中继兜底', itemStyle: { color: accent2 } }
      ]
    }]
  });
  window.addEventListener('resize', function() { chart.resize(); });
})();
